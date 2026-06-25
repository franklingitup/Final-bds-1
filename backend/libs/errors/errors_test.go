package errors

import (
	stderrors "errors"
	"net/http"
	"testing"
)

func TestHTTPStatusMapping(t *testing.T) {
	cases := map[Code]int{
		CodeUnauthenticated:  http.StatusUnauthorized,
		CodeForbidden:        http.StatusForbidden,
		CodeNotFound:         http.StatusNotFound,
		CodeValidationFailed: http.StatusUnprocessableEntity,
		CodeConflict:         http.StatusConflict,
		CodeRateLimited:      http.StatusTooManyRequests,
		CodeInternal:         http.StatusInternalServerError,
		Code("UNKNOWN"):      http.StatusInternalServerError,
	}
	for code, want := range cases {
		if got := HTTPStatus(code); got != want {
			t.Errorf("HTTPStatus(%s) = %d, want %d", code, got, want)
		}
	}
}

func TestWrapAndUnwrap(t *testing.T) {
	root := stderrors.New("boom")
	err := Wrap(root, CodeConflict, "already exists")

	if !stderrors.Is(err, root) {
		t.Fatal("expected wrapped cause to be discoverable via errors.Is")
	}
	if err.HTTPStatus() != http.StatusConflict {
		t.Errorf("status = %d", err.HTTPStatus())
	}
}

func TestFrom(t *testing.T) {
	if From(nil) != nil {
		t.Fatal("From(nil) must be nil")
	}

	app := NotFound("missing")
	if From(app) != app {
		t.Fatal("From should return the same *Error unchanged")
	}

	wrapped := From(stderrors.New("raw"))
	if wrapped.Code != CodeInternal {
		t.Errorf("expected INTERNAL, got %s", wrapped.Code)
	}
}

func TestEnvelopeOmitsCause(t *testing.T) {
	err := Wrap(stderrors.New("secret-internal-detail"), CodeValidationFailed, "bad input").
		WithDetails("field: email")

	env := err.Envelope("req-123")
	if env.Error.Code != CodeValidationFailed {
		t.Errorf("code = %s", env.Error.Code)
	}
	if env.Error.RequestID != "req-123" {
		t.Errorf("requestID = %s", env.Error.RequestID)
	}
	if len(env.Error.Details) != 1 || env.Error.Details[0] != "field: email" {
		t.Errorf("details = %v", env.Error.Details)
	}
	if env.Error.Message == "secret-internal-detail" {
		t.Error("internal cause leaked into envelope message")
	}
}

func TestWithDetailsDoesNotMutateOriginal(t *testing.T) {
	base := Validation("bad")
	_ = base.WithDetails("a")
	if len(base.Details) != 0 {
		t.Errorf("original mutated: %v", base.Details)
	}
}
