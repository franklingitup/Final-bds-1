package internalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetUsers_ResolvesSubset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/users" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("ids"); got == "" {
			t.Error("expected ids query")
		}
		_ = json.NewEncoder(w).Encode(usersResponse{Items: []UserSummary{
			{ID: "u1", Name: "Ada", Email: "ada@example.com"},
		}})
	}))
	defer srv.Close()

	c := NewAuthClient(srv.URL)
	got, err := c.GetUsers(context.Background(), []string{"u1", "missing", "u1", ""})
	if err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users, want 1", len(got))
	}
	if got["u1"].Name != "Ada" {
		t.Errorf("name = %q", got["u1"].Name)
	}
}

func TestGetUsers_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewAuthClient(srv.URL)
	_, err := c.GetUsers(context.Background(), []string{"u1"})
	if err == nil {
		t.Fatal("expected error on non-2xx")
	}
}

func TestGetUsers_EmptyIDsNoRequest(t *testing.T) {
	c := NewAuthClient("http://127.0.0.1:1")
	got, err := c.GetUsers(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty ids: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestGetUsers_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(usersResponse{})
	}))
	defer srv.Close()

	c := NewAuthClient(srv.URL)
	c.http.Timeout = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := c.GetUsers(ctx, []string{"u1"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestGetUsers_SendsInternalToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HeaderInternalToken) != "secret" {
			t.Errorf("token = %q", r.Header.Get(HeaderInternalToken))
		}
		_ = json.NewEncoder(w).Encode(usersResponse{})
	}))
	defer srv.Close()

	c := NewAuthClient(srv.URL)
	c.InternalToken = "secret"
	if _, err := c.GetUsers(context.Background(), []string{"u1"}); err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
}
