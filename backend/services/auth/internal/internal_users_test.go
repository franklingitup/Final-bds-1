package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/middleware"
)

func TestListInternalUsers_MixOfValidAndInvalidIDs(t *testing.T) {
	e := newTestEnv()
	mustSignup(t, e, "ada@example.com")
	u := e.users.byEmail["ada@example.com"]

	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler()})
	RegisterRoutes(app, NewHandler(e.svc))

	missing := uuid.NewString()
	url := "/internal/users?ids=" + u.ID + "," + missing + ",not-a-uuid," + u.ID
	req := httptest.NewRequest(http.MethodGet, url, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
	var parsed struct {
		Items []UserPublic `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(parsed.Items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(parsed.Items), parsed.Items)
	}
	got := parsed.Items[0]
	if got.ID != u.ID || got.Email != "ada@example.com" || got.Name != "Test User" {
		t.Errorf("item = %+v", got)
	}
	if got.AvatarURL != "" {
		t.Errorf("unexpected avatarUrl %q", got.AvatarURL)
	}
}

func TestListInternalUsers_RejectsBadInternalToken(t *testing.T) {
	e := newTestEnv()
	h := NewHandler(e.svc).WithInternalToken("secret")
	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler()})
	RegisterRoutes(app, h)

	req := httptest.NewRequest(http.MethodGet, "/internal/users?ids="+uuid.NewString(), nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestLookupUsers_SkipsUnknown(t *testing.T) {
	e := newTestEnv()
	mustSignup(t, e, "ada@example.com")
	u := e.users.byEmail["ada@example.com"]
	got, err := e.svc.LookupUsers(context.Background(), []string{u.ID, uuid.NewString(), "system"})
	if err != nil {
		t.Fatalf("LookupUsers: %v", err)
	}
	if len(got) != 1 || got[0].ID != u.ID {
		t.Fatalf("got %+v", got)
	}
}
