package deployment

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

const (
	// BuildConsumerDurable is the stable JetStream consumer name. Replicas that
	// share it form a queue group, so each build.succeeded event is handled by
	// exactly one instance (and deduplicated regardless via the processed-events
	// ledger).
	BuildConsumerDurable = "deployment-build-consumer"

	// eventBuildSucceeded is the event type owned by the Build service. The
	// deployment service integrates against the wire contract (event type + JSON
	// payload), not the build Go package, so the two services stay decoupled.
	eventBuildSucceeded = "build.succeeded"
)

// Consumer outcome labels for the deployment_build_events_total metric.
const (
	outcomeProcessed    = "processed"
	outcomeDeduplicated = "deduplicated"
	outcomeInvalid      = "invalid"
	outcomeIgnored      = "ignored"
	outcomeError        = "error"
)

var (
	buildEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "deployment_build_events_total",
		Help: "build.succeeded events handled by the deployment consumer, by outcome.",
	}, []string{"outcome"})

	buildReleasesCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_build_releases_created_total",
		Help: "Releases created by the deployment build consumer from build.succeeded events.",
	})

	buildEventDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "deployment_build_event_duration_seconds",
		Help:    "Wall-clock time to handle a build.succeeded event.",
		Buckets: prometheus.DefBuckets,
	})
)

// buildSucceededEvent is the deployment service's read model of the Build
// service's build.succeeded payload. We own our copy of the contract rather
// than importing the build package.
type buildSucceededEvent struct {
	BuildID     string `json:"buildId"`
	ImageDigest string `json:"imageDigest"`
	ImageTag    string `json:"imageTag"`
	Image       string `json:"image"`
	Registry    string `json:"registry"`
}

// BuildConsumerDeps are the dependencies of the build consumer. The stores are
// the same interfaces the service uses, so the consumer is unit-testable with
// the existing in-memory fakes.
type BuildConsumerDeps struct {
	Deployments  DeploymentStore
	Releases     ReleaseStore
	Processed    ProcessedEventStore
	Outbox       events.Outbox
	Tenant       TenantRunner
	Subscriber   events.Subscriber
	SubjectPrefix string
	Logger       *slog.Logger
	Now          func() time.Time
}

// BuildConsumer subscribes to build.succeeded events and rolls the freshly built
// image out to the deployments that run it: it creates a new immutable Release
// pinned to the build's digest, points the deployment at that digest, and resets
// it to pending so the agent's derived desired state serves the new revision.
//
// Delivery is at-least-once, so the handler is idempotent: it records each
// event id in a processed-events ledger inside the same transaction as the
// release/deployment writes, and acknowledges only after that transaction
// commits. A transient failure returns an error so the framework retries and
// eventually dead-letters.
type BuildConsumer struct {
	deps       DeploymentStore
	rels       ReleaseStore
	processed  ProcessedEventStore
	outbox     events.Outbox
	tenant     TenantRunner
	subscriber events.Subscriber
	prefix     string
	log        *slog.Logger
	now        func() time.Time
	sub        events.Subscription
}

// NewBuildConsumer wires a BuildConsumer from its dependencies.
func NewBuildConsumer(d BuildConsumerDeps) *BuildConsumer {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &BuildConsumer{
		deps:       d.Deployments,
		rels:       d.Releases,
		processed:  d.Processed,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		subscriber: d.Subscriber,
		prefix:     d.SubjectPrefix,
		log:        d.Logger,
		now:        d.Now,
	}
}

// Start registers the durable subscription filtered to build.succeeded events of
// any version and begins consuming.
func (c *BuildConsumer) Start(ctx context.Context) error {
	filter := eventBuildSucceeded + ".*" // any schema version, e.g. build.succeeded.v1
	if c.prefix != "" {
		filter = c.prefix + "." + filter
	}
	sub, err := c.subscriber.Subscribe(ctx, events.SubscriptionOptions{
		Durable:       BuildConsumerDurable,
		FilterSubject: filter,
	}, c.handle)
	if err != nil {
		return err
	}
	c.sub = sub
	return nil
}

// Stop halts delivery. In-flight handlers are allowed to finish.
func (c *BuildConsumer) Stop() {
	if c.sub != nil {
		c.sub.Stop()
	}
}

// buildProcessResult summarizes one event's handling for metrics and logging.
type buildProcessResult struct {
	buildID         string
	outcome         string
	matched         int
	releasesCreated int
}

func (c *BuildConsumer) handle(ctx context.Context, e events.Envelope) error {
	start := c.now()
	defer func() { buildEventDuration.Observe(c.now().Sub(start).Seconds()) }()

	// Continue the originating build's distributed trace across the async
	// boundary so the consumer span is a child of the build span.
	if e.TraceParent != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": e.TraceParent})
	}
	ctx, span := telemetry.Tracer("deployment-build-consumer").Start(ctx, "build.succeeded consume")
	defer span.End()
	span.SetAttributes(
		attribute.String("event.id", e.EventID),
		attribute.String("event.type", e.Type),
		attribute.String("org.id", e.OrgID),
	)

	// Defensive: the subject filter should deliver only build.succeeded, but a
	// broadened filter must never misroute work here.
	if e.Type != eventBuildSucceeded {
		buildEventsTotal.WithLabelValues(outcomeIgnored).Inc()
		return nil
	}

	res, err := c.process(ctx, e)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		buildEventsTotal.WithLabelValues(outcomeError).Inc()
		c.log.ErrorContext(ctx, "build consumer transaction failed",
			"eventId", e.EventID, "buildId", res.buildID, "error", err)
		return err // retried with backoff, then dead-lettered by the framework
	}

	buildEventsTotal.WithLabelValues(res.outcome).Inc()
	if res.releasesCreated > 0 {
		buildReleasesCreated.Add(float64(res.releasesCreated))
	}
	span.SetAttributes(
		attribute.String("consumer.outcome", res.outcome),
		attribute.Int("deployments.matched", res.matched),
		attribute.Int("releases.created", res.releasesCreated),
	)
	c.log.InfoContext(ctx, "build.succeeded handled",
		"eventId", e.EventID, "buildId", res.buildID, "outcome", res.outcome,
		"matched", res.matched, "releasesCreated", res.releasesCreated)
	return nil
}

// process performs the transactional work for one build.succeeded event.
// Validation failures that retrying cannot fix are acknowledged (returned with a
// nil error and an "invalid" outcome); only transient/storage errors are
// returned for retry.
func (c *BuildConsumer) process(ctx context.Context, e events.Envelope) (buildProcessResult, error) {
	var res buildProcessResult

	if e.OrgID == "" {
		// Cannot RLS-scope an event without a tenant; the publisher validates
		// this, so treat it as a poison message and acknowledge.
		res.outcome = outcomeInvalid
		c.log.WarnContext(ctx, "build.succeeded missing orgId", "eventId", e.EventID)
		return res, nil
	}

	payload, err := events.DecodePayload[buildSucceededEvent](e)
	if err != nil {
		res.outcome = outcomeInvalid
		c.log.WarnContext(ctx, "build.succeeded payload decode failed",
			"eventId", e.EventID, "error", err)
		return res, nil
	}
	res.buildID = payload.BuildID

	if payload.ImageDigest == "" || payload.Image == "" {
		res.outcome = outcomeInvalid
		c.log.WarnContext(ctx, "build.succeeded missing image or digest",
			"eventId", e.EventID, "buildId", payload.BuildID)
		return res, nil
	}

	buildRepo := imageRepository(composeImageRef(payload.Registry, payload.Image))
	pinnedImage := buildRepo + "@" + payload.ImageDigest

	txErr := c.tenant.WithTenant(ctx, e.OrgID, func(ctx context.Context) error {
		// Idempotency guard: record the event id first. A duplicate delivery is a
		// no-op that still acknowledges (nothing to roll out again).
		inserted, err := c.processed.MarkProcessed(ctx, BuildConsumerDurable, e.EventID, e.OrgID)
		if err != nil {
			return err
		}
		if !inserted {
			res.outcome = outcomeDeduplicated
			return nil
		}

		deployments, err := c.deps.ListAllActive(ctx)
		if err != nil {
			return err
		}

		for i := range deployments {
			d := deployments[i]
			if imageRepository(d.Image) != buildRepo {
				continue
			}
			res.matched++

			nextRevision := 1
			latest, err := c.rels.GetLatest(ctx, d.ID)
			switch {
			case err == nil && latest != nil:
				nextRevision = latest.Revision + 1
			case err != nil && !database.IsNotFound(err):
				return err
			}

			config, _ := json.Marshal(map[string]any{
				"image":         pinnedImage,
				"replicas":      d.Replicas,
				"cpuRequest":    d.CPURequest,
				"cpuLimit":      d.CPULimit,
				"memoryRequest": d.MemoryRequest,
				"memoryLimit":   d.MemoryLimit,
				"port":          d.Port,
				"envVars":       json.RawMessage(d.EnvVars),
			})

			rel := &Release{
				OrgID:        e.OrgID,
				DeploymentID: d.ID,
				Revision:     nextRevision,
				Image:        pinnedImage,
				Replicas:     d.Replicas,
				ConfigHash:   hashConfig(config),
				Config:       config,
				Status:       ReleaseStatusPending,
			}
			if err := c.rels.Create(ctx, rel); err != nil {
				return err
			}

			// Point the deployment at the immutable digest and reset it to
			// pending so the derived desired state the agent pulls (latest
			// release per deployment) reflects the new revision.
			d.Image = pinnedImage
			if err := c.deps.Update(ctx, &d); err != nil {
				return err
			}
			if err := c.deps.UpdateStatus(ctx, d.ID, StatusPending, nil, nil); err != nil {
				return err
			}

			// Emit deployment.created for the new revision, carrying the build's
			// correlation/trace context forward. Written to the outbox in the
			// same transaction so it publishes iff the rollout commits.
			evt, err := events.New(EventDeploymentCreated, eventVersion, e.OrgID, deploymentCreatedPayload{
				DeploymentID:  d.ID,
				ApplicationID: d.ApplicationID,
				ClusterID:     d.ClusterID,
				Image:         d.Image,
				Replicas:      d.Replicas,
				Revision:      nextRevision,
			},
				events.WithActor(events.Actor{Type: "system", ID: BuildConsumerDurable}),
				events.WithResource(events.Resource{Type: "deployment", ID: d.ID}),
				events.WithCorrelationID(e.CorrelationID),
				events.WithTraceParent(e.TraceParent),
			)
			if err != nil {
				return err
			}
			if err := c.outbox.Enqueue(ctx, evt); err != nil {
				return err
			}
			res.releasesCreated++
		}

		res.outcome = outcomeProcessed
		return nil
	})
	if txErr != nil {
		return res, txErr
	}
	return res, nil
}

// composeImageRef joins a registry host and image path into a single reference.
// If the registry is empty, or the image already carries its own registry host
// (its first path segment looks like a hostname), the image is returned
// unchanged.
func composeImageRef(registry, image string) string {
	image = strings.TrimSpace(image)
	registry = strings.TrimSpace(registry)
	if registry == "" || image == "" {
		return image
	}
	if strings.HasPrefix(image, registry+"/") {
		return image
	}
	if firstSeg := strings.SplitN(image, "/", 2)[0]; strings.ContainsAny(firstSeg, ".:") || firstSeg == "localhost" {
		return image
	}
	return registry + "/" + image
}

// imageRepository strips any tag and digest from an image reference, returning
// the bare repository (registry/name). This lets a build match the deployments
// that run its image regardless of the tag or digest they currently pin.
func imageRepository(ref string) string {
	ref = strings.TrimSpace(ref)
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	lastSlash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > lastSlash {
		ref = ref[:colon]
	}
	return ref
}
