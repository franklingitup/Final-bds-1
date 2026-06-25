package database

import (
	"context"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/config"
)

func TestPoolConfig_RequiresURL(t *testing.T) {
	if _, err := poolConfig(config.DatabaseConfig{}); err == nil {
		t.Fatal("expected error for empty DATABASE_URL")
	}
}

func TestPoolConfig_AppliesSettings(t *testing.T) {
	cfg := config.DatabaseConfig{
		URL:             "postgres://u:p@localhost:5432/db?sslmode=disable",
		MaxConns:        20,
		MinConns:        4,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 10 * time.Minute,
	}
	pc, err := poolConfig(cfg)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	if pc.MaxConns != 20 || pc.MinConns != 4 {
		t.Errorf("conns = %d/%d", pc.MaxConns, pc.MinConns)
	}
	if pc.MaxConnLifetime != time.Hour {
		t.Errorf("lifetime = %v", pc.MaxConnLifetime)
	}
}

func TestPoolConfig_InvalidURL(t *testing.T) {
	if _, err := poolConfig(config.DatabaseConfig{URL: "://nope", MaxConns: 1}); err == nil {
		t.Fatal("expected parse error for invalid URL")
	}
}

func TestConnect_EmptyURL(t *testing.T) {
	if _, err := Connect(context.Background(), config.Config{}); err == nil {
		t.Fatal("expected error connecting with empty URL")
	}
}

func TestNewRedisClient_EmptyURL(t *testing.T) {
	if _, err := NewRedisClient(context.Background(), config.Config{}); err == nil {
		t.Fatal("expected error for empty REDIS_URL")
	}
}
