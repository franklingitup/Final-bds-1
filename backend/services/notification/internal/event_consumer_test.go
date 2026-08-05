package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

// fakeDispatcher records the notifications the consumer would persist and can be
// primed to fail, standing in for *Service.dispatchTx.
type fakeDispatcher struct {
	created []*Notification
	err     error
}

func (f *fakeDispatcher) dispatchTx(ctx context.Context, n *Notification) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, n)
	return nil
}

type fakeProcessedEventStore struct {
	seen map[string]bool
	err  error
}

func newFakeProcessedEventStore() *fakeProcessedEventStore {
	return &fakeProcessedEventStore{seen: make(map[string]bool)}
}

func (f *fakeProcessedEventStore) MarkProcessed(ctx context.Context, consumer, eventID, orgID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	key := consumer + "|" + eventID
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

// fakeTenant runs the function without a real transaction.
type fakeTenant struct{}

func (fakeTenant) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

func newTestDeploymentConsumer() (*DeploymentConsumer, *fakeDispatcher, *fakeProcessedEventStore) {
	disp := &fakeDispatcher{}
	processed := newFakeProcessedEventStore()
	c := NewDeploymentConsumer(DeploymentConsumerDeps{
		Dispatcher: disp,
		Processed:  processed,
		Tenant:     fakeTenant{},
	})
	return c, disp, processed
}

func deploymentEnvelope(t *testing.T, eventType, orgID string, p deploymentEventPayload) events.Envelope {
	t.Helper()
	e, err := events.New(eventType, 1, orgID, p)
	require.NoError(t, err)
	return e
}

// ----------------------------------------------------------------------------
// Mapping / rendering behavior
// ----------------------------------------------------------------------------

func TestDeploymentConsumer_SucceededCreatesNotification(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	e := deploymentEnvelope(t, srcDeploymentSucceeded, "org-1", deploymentEventPayload{
		DeploymentID: "dep-1", Revision: 5, ReadyReplicas: 3,
	})
	require.NoError(t, c.handle(ctx, e))

	require.Len(t, disp.created, 1)
	n := disp.created[0]
	assert.Equal(t, EventDeploymentCompleted, n.EventType, "deployment.succeeded maps to the notification taxonomy")
	assert.Equal(t, SeverityInfo, n.Severity)
	assert.Equal(t, "org-1", n.OrgID)
	assert.Equal(t, StatusPending, n.Status)
	require.NotNil(t, n.ResourceID)
	assert.Equal(t, "dep-1", *n.ResourceID)
	require.NotNil(t, n.ResourceType)
	assert.Equal(t, "deployment", *n.ResourceType)
	require.NotNil(t, n.EventID)
	assert.Equal(t, e.EventID, *n.EventID, "notification carries the source event id for idempotency/traceability")
	assert.NotEmpty(t, n.Title)
	assert.Contains(t, n.Body, "revision 5")
}

func TestDeploymentConsumer_FailedUsesErrorAndSeverity(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	e := deploymentEnvelope(t, srcDeploymentFailed, "org-1", deploymentEventPayload{
		DeploymentID: "dep-1", Revision: 2, ErrorMessage: "ImagePullBackOff",
	})
	require.NoError(t, c.handle(ctx, e))

	require.Len(t, disp.created, 1)
	n := disp.created[0]
	assert.Equal(t, EventDeploymentFailed, n.EventType)
	assert.Equal(t, SeverityError, n.Severity)
	assert.Contains(t, n.Body, "ImagePullBackOff")
}

func TestDeploymentConsumer_RollbackWarningWithoutTemplate(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	e := deploymentEnvelope(t, srcDeploymentRollback, "org-1", deploymentEventPayload{
		DeploymentID: "dep-1", FromRevision: 5, TargetRevision: 4,
	})
	require.NoError(t, c.handle(ctx, e))

	require.Len(t, disp.created, 1)
	n := disp.created[0]
	assert.Equal(t, EventDeploymentRolledBack, n.EventType)
	assert.Equal(t, SeverityWarning, n.Severity)
	assert.NotEmpty(t, n.Title, "must fall back to a generated title when no default template exists")
	assert.Contains(t, n.Body, "rolled back")
}

// ----------------------------------------------------------------------------
// Idempotency and routing
// ----------------------------------------------------------------------------

func TestDeploymentConsumer_DeduplicatesByEventID(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	e := deploymentEnvelope(t, srcDeploymentSucceeded, "org-1", deploymentEventPayload{DeploymentID: "dep-1", Revision: 1})
	require.NoError(t, c.handle(ctx, e))
	require.NoError(t, c.handle(ctx, e)) // redelivery of the same event id

	assert.Len(t, disp.created, 1, "duplicate delivery must not create a second notification")
}

func TestDeploymentConsumer_IgnoresUnmappedDeploymentEvent(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	// deployment.created is intentionally not notified on.
	e := deploymentEnvelope(t, "deployment.created", "org-1", deploymentEventPayload{DeploymentID: "dep-1"})
	require.NoError(t, c.handle(ctx, e))
	assert.Empty(t, disp.created)

	res, err := c.process(ctx, e)
	require.NoError(t, err)
	assert.Equal(t, outcomeIgnored, res.outcome)
}

func TestDeploymentConsumer_IgnoresForeignNamespace(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	e := deploymentEnvelope(t, "build.succeeded", "org-1", deploymentEventPayload{DeploymentID: "dep-1"})
	require.NoError(t, c.handle(ctx, e))
	assert.Empty(t, disp.created)
}

func TestDeploymentConsumer_InvalidPayloadIsAcked(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	// Missing deploymentId -> unfixable by retry -> acknowledged.
	e := deploymentEnvelope(t, srcDeploymentSucceeded, "org-1", deploymentEventPayload{})
	require.NoError(t, c.handle(ctx, e))
	assert.Empty(t, disp.created)

	res, err := c.process(ctx, e)
	require.NoError(t, err)
	assert.Equal(t, outcomeInvalid, res.outcome)
}

func TestDeploymentConsumer_MissingOrgIDIsAcked(t *testing.T) {
	c, _, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	// Constructed directly since events.New rejects an empty orgId.
	e := events.Envelope{
		EventID: "evt-1", Type: srcDeploymentSucceeded, Version: 1,
		Payload: []byte(`{"deploymentId":"dep-1"}`),
	}
	res, err := c.process(ctx, e)
	require.NoError(t, err)
	assert.Equal(t, outcomeInvalid, res.outcome)
}

func TestDeploymentConsumer_MalformedPayloadIsAcked(t *testing.T) {
	c, _, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	e := events.Envelope{
		EventID: "evt-2", Type: srcDeploymentSucceeded, Version: 1, OrgID: "org-1",
		Payload: []byte("{not-json"),
	}
	res, err := c.process(ctx, e)
	require.NoError(t, err)
	assert.Equal(t, outcomeInvalid, res.outcome)
}

// ----------------------------------------------------------------------------
// Retry semantics
// ----------------------------------------------------------------------------

func TestDeploymentConsumer_LedgerErrorIsRetried(t *testing.T) {
	c, _, processed := newTestDeploymentConsumer()
	ctx := context.Background()

	processed.err = errors.New("db unavailable")
	e := deploymentEnvelope(t, srcDeploymentSucceeded, "org-1", deploymentEventPayload{DeploymentID: "dep-1"})

	// A storage error must propagate so the framework retries / dead-letters.
	assert.Error(t, c.handle(ctx, e))
}

func TestDeploymentConsumer_DispatchErrorIsRetried(t *testing.T) {
	c, disp, _ := newTestDeploymentConsumer()
	ctx := context.Background()

	disp.err = errors.New("store unavailable")
	e := deploymentEnvelope(t, srcDeploymentSucceeded, "org-1", deploymentEventPayload{DeploymentID: "dep-1"})

	assert.Error(t, c.handle(ctx, e))
}
