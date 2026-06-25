package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/config"
)

func newTestLogger(t *testing.T) (*bytes.Buffer, func(ctx context.Context, msg string)) {
	t.Helper()
	buf := &bytes.Buffer{}
	cfg := config.Config{ServiceName: "test", Environment: config.EnvDevelopment, Log: config.LogConfig{Level: "debug", Format: "json"}}
	l := NewWithWriter(cfg, buf)
	return buf, func(ctx context.Context, msg string) { l.InfoContext(ctx, msg) }
}

func TestCorrelationIDInjected(t *testing.T) {
	buf, log := newTestLogger(t)
	ctx := WithCorrelationID(context.Background(), "corr-42")
	log(ctx, "hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log: %v (%s)", err, buf.String())
	}
	if entry["correlation_id"] != "corr-42" {
		t.Errorf("correlation_id = %v", entry["correlation_id"])
	}
	if entry["service"] != "test" {
		t.Errorf("service = %v", entry["service"])
	}
}

func TestNoCorrelationWhenAbsent(t *testing.T) {
	buf, log := newTestLogger(t)
	log(context.Background(), "hello")
	if strings.Contains(buf.String(), "correlation_id") {
		t.Errorf("unexpected correlation_id in %s", buf.String())
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "abc")
	if CorrelationID(ctx) != "abc" {
		t.Fatal("correlation round-trip failed")
	}
	if FromContext(context.Background()) == nil {
		t.Fatal("FromContext should fall back to default logger")
	}
}
