package secrets

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// fakePoolQuerier is a mock for PoolQuerier that returns configurable results.
type fakePoolQuerier struct {
	scanErr       error
	orgID         string
	storedAgentID *string
	status        string
}

type fakeRow struct {
	scanErr       error
	orgID         string
	storedAgentID *string
	status        string
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	// dest[0] = org_id, dest[1] = agent_id, dest[2] = status
	if len(dest) >= 3 {
		if p, ok := dest[0].(*string); ok {
			*p = r.orgID
		}
		if p, ok := dest[1].(**string); ok {
			*p = r.storedAgentID
		}
		if p, ok := dest[2].(*string); ok {
			*p = r.status
		}
	}
	return nil
}

func (f *fakePoolQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &fakeRow{
		scanErr:       f.scanErr,
		orgID:         f.orgID,
		storedAgentID: f.storedAgentID,
		status:        f.status,
	}
}

// TestValidateClusterReturnsIdenticalErrorMessages verifies that all failure
// cases return the same error message to prevent information leakage about
// cluster existence or status. This guards decrypted secret retrieval, so a
// distinguishable error here lets an attacker enumerate cluster IDs and their
// connection state across tenants.
func TestValidateClusterReturnsIdenticalErrorMessages(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()
	storedAgentID := "correct-agent"

	tests := []struct {
		name    string
		pool    *fakePoolQuerier
		agentID string
	}{
		{
			name: "cluster not found",
			pool: &fakePoolQuerier{
				scanErr: pgx.ErrNoRows,
			},
			agentID: "any-agent",
		},
		{
			name: "cluster not connected",
			pool: &fakePoolQuerier{
				orgID:         "org-123",
				storedAgentID: &storedAgentID,
				status:        "pending",
			},
			agentID: "correct-agent",
		},
		{
			name: "agent ID mismatch",
			pool: &fakePoolQuerier{
				orgID:         "org-123",
				storedAgentID: &storedAgentID,
				status:        "connected",
			},
			agentID: "wrong-agent",
		},
		{
			name: "agent ID is nil",
			pool: &fakePoolQuerier{
				orgID:         "org-123",
				storedAgentID: nil,
				status:        "connected",
			},
			agentID: "any-agent",
		},
	}

	var errorMessages []string
	var statusCodes []int
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewClusterValidator(tt.pool, log)
			_, err := validator.ValidateCluster(ctx, "cluster-123", tt.agentID)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			appErr := apperrors.From(err)
			if appErr.Code != apperrors.CodeUnauthenticated {
				t.Errorf("expected CodeUnauthenticated, got %s", appErr.Code)
			}

			errorMessages = append(errorMessages, appErr.Message)
			statusCodes = append(statusCodes, appErr.HTTPStatus())
		})
	}

	// Verify all error messages are identical.
	if len(errorMessages) < 2 {
		t.Fatal("expected at least 2 error messages to compare")
	}

	firstMsg := errorMessages[0]
	for i, msg := range errorMessages[1:] {
		if msg != firstMsg {
			t.Errorf("error message %d (%q) differs from first message (%q) - information leakage vulnerability",
				i+2, msg, firstMsg)
		}
	}

	// Verify all HTTP status codes are identical.
	firstStatus := statusCodes[0]
	for i, code := range statusCodes[1:] {
		if code != firstStatus {
			t.Errorf("status code %d (%d) differs from first status (%d) - information leakage vulnerability",
				i+2, code, firstStatus)
		}
	}

	// Verify the error message is generic (doesn't reveal internal details).
	if firstMsg == "" {
		t.Error("error message is empty")
	}
	for _, forbidden := range []string{"not found", "not connected", "mismatch", "credentials"} {
		if strings.Contains(firstMsg, forbidden) {
			t.Errorf("error message %q contains forbidden substring %q that may leak information",
				firstMsg, forbidden)
		}
	}
}

// TestValidateClusterSucceeds verifies the validator returns the org ID on success.
func TestValidateClusterSucceeds(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()
	storedAgentID := "correct-agent"

	pool := &fakePoolQuerier{
		orgID:         "org-123",
		storedAgentID: &storedAgentID,
		status:        "connected",
	}

	validator := NewClusterValidator(pool, log)
	orgID, err := validator.ValidateCluster(ctx, "cluster-123", "correct-agent")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orgID != "org-123" {
		t.Errorf("orgID = %q, want %q", orgID, "org-123")
	}
}

// TestNewClusterValidatorNilLoggerDefaults verifies a nil logger does not panic.
func TestNewClusterValidatorNilLoggerDefaults(t *testing.T) {
	ctx := context.Background()
	pool := &fakePoolQuerier{scanErr: pgx.ErrNoRows}

	validator := NewClusterValidator(pool, nil)
	if _, err := validator.ValidateCluster(ctx, "cluster-123", "any-agent"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
