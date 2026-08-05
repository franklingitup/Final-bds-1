package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL, AuthToken: "test-token", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestNewValidatesBaseURL(t *testing.T) {
	if _, err := New(Config{BaseURL: ""}); err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
	if _, err := New(Config{BaseURL: "https://argocd.example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateApplication(t *testing.T) {
	var gotAuth, gotMethod, gotPath string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.URL.Query().Get("upsert") != "true" {
			t.Errorf("expected upsert=true, got %q", r.URL.RawQuery)
		}
		var app Application
		_ = json.NewDecoder(r.Body).Decode(&app)
		app.Status.Sync.Status = SyncStatusOutOfSync
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(app)
	})

	app := &Application{Metadata: ObjectMeta{Name: "my-app"}, Spec: ApplicationSpec{Project: "default"}}
	out, err := c.CreateApplication(context.Background(), app)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if out.Metadata.Name != "my-app" {
		t.Errorf("name = %q", out.Metadata.Name)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/applications" {
		t.Errorf("method/path = %s %s", gotMethod, gotPath)
	}
}

func TestGetApplicationNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found","code":5,"message":"applications.argoproj.io \"x\" not found"}`))
	})
	_, err := c.GetApplication(context.Background(), "x")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetApplicationAPIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"permission denied"}`))
	})
	_, err := c.GetApplication(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Message != "permission denied" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestDeleteApplicationCascade(t *testing.T) {
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})
	if err := c.DeleteApplication(context.Background(), "my-app", true); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}
	if gotQuery != "cascade=true" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestSyncApplication(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/applications/my-app/sync" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var req SyncRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Revision != "abc123" {
			t.Errorf("revision = %q", req.Revision)
		}
		_ = json.NewEncoder(w).Encode(Application{
			Metadata: ObjectMeta{Name: "my-app"},
			Status:   ApplicationStatus{OperationState: &OperationState{Phase: OperationRunning}},
		})
	})
	out, err := c.SyncApplication(context.Background(), "my-app", SyncRequest{Revision: "abc123", Prune: true})
	if err != nil {
		t.Fatalf("SyncApplication: %v", err)
	}
	if out.OperationPhase() != OperationRunning {
		t.Errorf("phase = %q", out.OperationPhase())
	}
}

func TestRefreshApplication(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("refresh") != "normal" {
			t.Errorf("refresh = %q", r.URL.Query().Get("refresh"))
		}
		_ = json.NewEncoder(w).Encode(Application{Metadata: ObjectMeta{Name: "my-app"}})
	})
	if _, err := c.RefreshApplication(context.Background(), "my-app", false); err != nil {
		t.Fatalf("RefreshApplication: %v", err)
	}
}

func TestTerminateOperation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/applications/my-app/operation" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := c.TerminateOperation(context.Background(), "my-app"); err != nil {
		t.Fatalf("TerminateOperation: %v", err)
	}
}

func TestRollbackApplication(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req RollbackRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID != 7 {
			t.Errorf("id = %d", req.ID)
		}
		_ = json.NewEncoder(w).Encode(Application{Metadata: ObjectMeta{Name: "my-app"}})
	})
	if _, err := c.RollbackApplication(context.Background(), "my-app", 7); err != nil {
		t.Fatalf("RollbackApplication: %v", err)
	}
}

func TestListApplications(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("selector"); got != "app.kubernetes.io/managed-by=bdsplatform" {
			t.Errorf("selector = %q", got)
		}
		_ = json.NewEncoder(w).Encode(applicationList{Items: []Application{
			{Metadata: ObjectMeta{Name: "a"}},
			{Metadata: ObjectMeta{Name: "b"}},
		}})
	})
	apps, err := c.ListApplications(context.Background(), ListOptions{Selector: "app.kubernetes.io/managed-by=bdsplatform"})
	if err != nil {
		t.Fatalf("ListApplications: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("len = %d", len(apps))
	}
}

func TestWaitForSync(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		status := SyncStatusOutOfSync
		if n >= 3 {
			status = SyncStatusSynced
		}
		_ = json.NewEncoder(w).Encode(Application{
			Metadata: ObjectMeta{Name: "my-app"},
			Status:   ApplicationStatus{Sync: SyncStatus{Status: status, Revision: "rev"}},
		})
	})
	out, err := c.WaitForSync(context.Background(), "my-app", WaitOptions{Interval: time.Millisecond, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("WaitForSync: %v", err)
	}
	if !out.IsSynced() {
		t.Errorf("not synced: %+v", out.Status.Sync)
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Errorf("expected >=3 polls, got %d", calls)
	}
}

func TestWaitForHealthyTimeout(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Application{
			Metadata: ObjectMeta{Name: "my-app"},
			Status:   ApplicationStatus{Health: HealthStatus{Status: HealthStatusProgressing}},
		})
	})
	_, err := c.WaitForHealthy(context.Background(), "my-app", WaitOptions{Interval: time.Millisecond, Timeout: 30 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRetryOperationRetriesTransient(t *testing.T) {
	var calls int
	c, _ := newTestClient(t, nil)
	err := c.RetryOperation(context.Background(), RetryOptions{MaxAttempts: 4, BaseDelay: time.Millisecond}, func(context.Context) error {
		calls++
		if calls < 3 {
			return &APIError{StatusCode: http.StatusServiceUnavailable, Message: "unavailable"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetryOperation: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryOperationDoesNotRetryClientError(t *testing.T) {
	var calls int
	c, _ := newTestClient(t, nil)
	err := c.RetryOperation(context.Background(), RetryOptions{MaxAttempts: 4, BaseDelay: time.Millisecond}, func(context.Context) error {
		calls++
		return &APIError{StatusCode: http.StatusBadRequest, Message: "bad"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", calls)
	}
}

func TestFindHistoryID(t *testing.T) {
	app := &Application{Status: ApplicationStatus{History: []RevisionHistory{
		{ID: 1, Revision: "old"},
		{ID: 2, Revision: "target"},
		{ID: 3, Revision: "current"},
		{ID: 4, Revision: "target"}, // most recent match wins
	}}}
	id, ok := FindHistoryID(app, "target")
	if !ok || id != 4 {
		t.Fatalf("id=%d ok=%v, want 4 true", id, ok)
	}
	if _, ok := FindHistoryID(app, "missing"); ok {
		t.Fatal("expected no match for missing revision")
	}
}
