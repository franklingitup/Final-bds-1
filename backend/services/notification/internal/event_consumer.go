package notification

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

const (
	// DeploymentConsumerDurable is the stable JetStream consumer name. Replicas
	// that share it form a queue group so each deployment event is handled by
	// exactly one instance; redeliveries are additionally deduplicated via the
	// processed-events ledger.
	DeploymentConsumerDurable = "notification-deployment-consumer"

	// deploymentSubjectFilter matches every deployment.* event of any schema
	// version. The Deployment service owns the "deployment.*" namespace and
	// publishes subjects like "deployment.succeeded.v1"; ">" matches the dotted
	// type plus the trailing version token(s). The consumer subscribes broadly
	// and maps/ignores individual types so adding a new deployment event never
	// requires a subscription change.
	deploymentSubjectFilter = "deployment.>"
)

// Deployment event types owned by the Deployment service. The notification
// service integrates against the wire contract (event type + JSON payload), not
// the deployment Go package, so the two services stay decoupled.
const (
	srcDeploymentStarted   = "deployment.started"
	srcDeploymentSucceeded = "deployment.succeeded"
	srcDeploymentFailed    = "deployment.failed"
	srcDeploymentRollback  = "deployment.rollback.requested"
)

// Consumer outcome labels for the notification_deployment_events_total metric.
const (
	outcomeNotified     = "notified"
	outcomeDeduplicated = "deduplicated"
	outcomeInvalid      = "invalid"
	outcomeIgnored      = "ignored"
	outcomeError        = "error"
)

var (
	deploymentEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notification_deployment_events_total",
		Help: "Deployment events handled by the notification consumer, by outcome.",
	}, []string{"outcome"})

	deploymentNotificationsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "notification_deployment_notifications_created_total",
		Help: "Notifications created by the notification deployment consumer.",
	})

	deploymentEventDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "notification_deployment_event_duration_seconds",
		Help:    "Wall-clock time to handle a deployment event.",
		Buckets: prometheus.DefBuckets,
	})
)

// eventMapping describes how a source deployment event type maps onto the
// notification service's own event taxonomy (used for templates, preferences
// and webhook subscriptions) and its severity.
type eventMapping struct {
	notifEventType string
	severity       string
}

// deploymentEventMappings translates the Deployment service's emitted event
// types into the notification taxonomy. Only the lifecycle transitions users
// care about are notified on; internal events such as deployment.created (which
// the desired-state machinery emits on every revision) are intentionally
// ignored to avoid notification noise.
var deploymentEventMappings = map[string]eventMapping{
	srcDeploymentStarted:   {EventDeploymentStarted, SeverityInfo},
	srcDeploymentSucceeded: {EventDeploymentCompleted, SeverityInfo},
	srcDeploymentFailed:    {EventDeploymentFailed, SeverityError},
	srcDeploymentRollback:  {EventDeploymentRolledBack, SeverityWarning},
}

// deploymentEventPayload is the notification service's read model of the union
// of the Deployment service's deployment.* payloads. Each source event only
// populates the subset of fields relevant to it; unset fields decode to zero.
type deploymentEventPayload struct {
	DeploymentID   string `json:"deploymentId"`
	ReleaseID      string `json:"releaseId"`
	Revision       int    `json:"revision"`
	Image          string `json:"image"`
	ReadyReplicas  int    `json:"readyReplicas"`
	ErrorMessage   string `json:"errorMessage"`
	FromRevision   int    `json:"fromRevision"`
	TargetRevision int    `json:"targetRevision"`
}

// notificationDispatcher persists a notification and fans out its deliveries.
// It must be invoked inside a tenant-scoped transaction. *Service implements it
// via dispatchTx; the narrow interface keeps the consumer unit-testable without
// standing up the full store set.
type notificationDispatcher interface {
	dispatchTx(ctx context.Context, n *Notification) error
}

// DeploymentConsumerDeps are the dependencies of the deployment event consumer.
type DeploymentConsumerDeps struct {
	Dispatcher    notificationDispatcher
	Processed     ProcessedEventStore
	Tenant        TenantRunner
	Subscriber    events.Subscriber
	SubjectPrefix string
	Logger        *slog.Logger
	Now           func() time.Time
}

// DeploymentConsumer subscribes to deployment lifecycle events and turns each
// into a notification, which the delivery worker then fans out to the org's
// configured channels and webhooks.
//
// Delivery is at-least-once, so the handler is idempotent: it records each
// event id in a processed-events ledger inside the same transaction as the
// notification write, and acknowledges only after that transaction commits.
// Unfixable messages (missing tenant/payload, or an event type we don't notify
// on) are acknowledged; only transient storage errors are returned so the
// framework retries and eventually dead-letters.
type DeploymentConsumer struct {
	dispatcher notificationDispatcher
	processed  ProcessedEventStore
	tenant     TenantRunner
	subscriber events.Subscriber
	prefix     string
	engine     *TemplateEngine
	log        *slog.Logger
	now        func() time.Time
	sub        events.Subscription
}

// NewDeploymentConsumer wires a DeploymentConsumer from its dependencies.
func NewDeploymentConsumer(d DeploymentConsumerDeps) *DeploymentConsumer {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &DeploymentConsumer{
		dispatcher: d.Dispatcher,
		processed:  d.Processed,
		tenant:     d.Tenant,
		subscriber: d.Subscriber,
		prefix:     d.SubjectPrefix,
		engine:     NewTemplateEngine(),
		log:        d.Logger,
		now:        d.Now,
	}
}

// Start registers the durable subscription filtered to deployment.* events and
// begins consuming.
func (c *DeploymentConsumer) Start(ctx context.Context) error {
	filter := deploymentSubjectFilter
	if c.prefix != "" {
		filter = c.prefix + "." + filter
	}
	sub, err := c.subscriber.Subscribe(ctx, events.SubscriptionOptions{
		Durable:       DeploymentConsumerDurable,
		FilterSubject: filter,
	}, c.handle)
	if err != nil {
		return err
	}
	c.sub = sub
	return nil
}

// Stop halts delivery. In-flight handlers are allowed to finish.
func (c *DeploymentConsumer) Stop() {
	if c.sub != nil {
		c.sub.Stop()
	}
}

// deploymentProcessResult summarizes one event's handling for metrics/logging.
type deploymentProcessResult struct {
	deploymentID string
	outcome      string
}

func (c *DeploymentConsumer) handle(ctx context.Context, e events.Envelope) error {
	start := c.now()
	defer func() { deploymentEventDuration.Observe(c.now().Sub(start).Seconds()) }()

	// Continue the originating deployment's distributed trace across the async
	// boundary so the consumer span is a child of the deployment span.
	if e.TraceParent != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": e.TraceParent})
	}
	ctx, span := telemetry.Tracer("notification-deployment-consumer").Start(ctx, "deployment event consume")
	defer span.End()
	span.SetAttributes(
		attribute.String("event.id", e.EventID),
		attribute.String("event.type", e.Type),
		attribute.String("org.id", e.OrgID),
	)

	res, err := c.process(ctx, e)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		deploymentEventsTotal.WithLabelValues(outcomeError).Inc()
		c.log.ErrorContext(ctx, "notification deployment consumer transaction failed",
			"eventId", e.EventID, "eventType", e.Type, "deploymentId", res.deploymentID, "error", err)
		return err // retried with backoff, then dead-lettered by the framework
	}

	deploymentEventsTotal.WithLabelValues(res.outcome).Inc()
	if res.outcome == outcomeNotified {
		deploymentNotificationsCreated.Inc()
	}
	span.SetAttributes(attribute.String("consumer.outcome", res.outcome))
	c.log.InfoContext(ctx, "deployment event handled",
		"eventId", e.EventID, "eventType", e.Type, "deploymentId", res.deploymentID,
		"outcome", res.outcome)
	return nil
}

// process performs the transactional work for one deployment event. Validation
// failures that retrying cannot fix are acknowledged (nil error with an
// "invalid"/"ignored" outcome); only transient/storage errors are returned for
// retry.
func (c *DeploymentConsumer) process(ctx context.Context, e events.Envelope) (deploymentProcessResult, error) {
	var res deploymentProcessResult

	mapping, ok := deploymentEventMappings[e.Type]
	if !ok {
		// A deployment.* event we deliberately don't notify on (e.g.
		// deployment.created). Acknowledge without work.
		res.outcome = outcomeIgnored
		return res, nil
	}

	if e.OrgID == "" {
		// Cannot RLS-scope an event without a tenant; the publisher validates
		// this, so treat it as a poison message and acknowledge.
		res.outcome = outcomeInvalid
		c.log.WarnContext(ctx, "deployment event missing orgId", "eventId", e.EventID, "eventType", e.Type)
		return res, nil
	}

	payload, err := events.DecodePayload[deploymentEventPayload](e)
	if err != nil {
		res.outcome = outcomeInvalid
		c.log.WarnContext(ctx, "deployment event payload decode failed",
			"eventId", e.EventID, "eventType", e.Type, "error", err)
		return res, nil
	}
	res.deploymentID = payload.DeploymentID

	if payload.DeploymentID == "" {
		res.outcome = outcomeInvalid
		c.log.WarnContext(ctx, "deployment event missing deploymentId",
			"eventId", e.EventID, "eventType", e.Type)
		return res, nil
	}

	notif := c.buildNotification(e, mapping, payload)

	txErr := c.tenant.WithTenant(ctx, e.OrgID, func(ctx context.Context) error {
		// Idempotency guard: record the event id first. A duplicate delivery is
		// a no-op that still acknowledges (the notification already exists).
		inserted, err := c.processed.MarkProcessed(ctx, DeploymentConsumerDurable, e.EventID, e.OrgID)
		if err != nil {
			return err
		}
		if !inserted {
			res.outcome = outcomeDeduplicated
			return nil
		}
		if err := c.dispatcher.dispatchTx(ctx, notif); err != nil {
			return err
		}
		res.outcome = outcomeNotified
		return nil
	})
	if txErr != nil {
		return res, txErr
	}
	return res, nil
}

// buildNotification renders a notification from a deployment event using the
// mapped notification event type. It never touches storage, so it can run
// before the transaction is opened.
func (c *DeploymentConsumer) buildNotification(e events.Envelope, m eventMapping, p deploymentEventPayload) *Notification {
	title, body := c.render(m, p)

	metadata := fmt.Sprintf(
		`{"sourceEvent":%q,"sourceEventId":%q,"deploymentId":%q,"revision":%d}`,
		e.Type, e.EventID, p.DeploymentID, p.Revision,
	)

	eventID := e.EventID
	resourceType := "deployment"
	resourceID := p.DeploymentID

	return &Notification{
		OrgID:        e.OrgID,
		EventType:    m.notifEventType,
		EventID:      &eventID,
		Title:        title,
		Body:         body,
		Severity:     m.severity,
		ResourceType: &resourceType,
		ResourceID:   &resourceID,
		Metadata:     []byte(metadata),
		Status:       StatusPending,
	}
}

// render produces the notification title and body. It prefers the in-code
// default template for the mapped event type (rendered via the template
// engine) and falls back to a plain generated summary when no template exists
// or rendering fails, so a template problem can never drop a notification.
func (c *DeploymentConsumer) render(m eventMapping, p deploymentEventPayload) (title, body string) {
	summary := deploymentSummary(m.notifEventType, p)

	data := &RenderData{
		EventType:      m.notifEventType,
		Severity:       m.severity,
		ResourceType:   "deployment",
		ResourceID:     p.DeploymentID,
		ResourceName:   p.DeploymentID,
		DeploymentName: p.DeploymentID,
		Version:        revisionLabel(p.Revision),
		Body:           summary,
	}

	tmpl, ok := DefaultTemplates[m.notifEventType]
	if !ok {
		return summary, summary
	}

	title, err := c.engine.RenderSubject(tmpl.Subject, data)
	if err != nil || strings.TrimSpace(title) == "" {
		title = summary
	}
	body, err = c.engine.Render(tmpl.BodyText, data)
	if err != nil || strings.TrimSpace(body) == "" {
		body = summary
	}
	return title, body
}

// deploymentSummary builds a human-readable one-liner from the event payload,
// used both as the notification fallback body and (for failures) as the error
// text the default template interpolates.
func deploymentSummary(notifEventType string, p deploymentEventPayload) string {
	switch notifEventType {
	case EventDeploymentStarted:
		return fmt.Sprintf("Deployment %s started rolling out %s.", p.DeploymentID, revisionLabel(p.Revision))
	case EventDeploymentCompleted:
		return fmt.Sprintf("Deployment %s succeeded at %s (%d replicas ready).",
			p.DeploymentID, revisionLabel(p.Revision), p.ReadyReplicas)
	case EventDeploymentFailed:
		if strings.TrimSpace(p.ErrorMessage) != "" {
			return p.ErrorMessage
		}
		return fmt.Sprintf("Deployment %s failed at %s.", p.DeploymentID, revisionLabel(p.Revision))
	case EventDeploymentRolledBack:
		return fmt.Sprintf("Deployment %s rolled back from revision %d to revision %d.",
			p.DeploymentID, p.FromRevision, p.TargetRevision)
	default:
		return fmt.Sprintf("Deployment %s event.", p.DeploymentID)
	}
}

// revisionLabel renders a revision number, tolerating events (e.g. rollback
// requests) that don't carry one.
func revisionLabel(revision int) string {
	if revision <= 0 {
		return "a new revision"
	}
	return fmt.Sprintf("revision %d", revision)
}
