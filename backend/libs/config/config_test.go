package config

import (
	"testing"
	"time"
)

func baseValid() Config {
	return Config{
		ServiceName:     "auth",
		Environment:     EnvDevelopment,
		HTTPAddr:        ":8080",
		ShutdownTimeout: 15 * time.Second,
		Log:             LogConfig{Level: "info", Format: "json"},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := baseValid().Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	tests := map[string]func(*Config){
		"empty service name": func(c *Config) { c.ServiceName = "" },
		"bad environment":    func(c *Config) { c.Environment = "qa" },
		"empty addr":         func(c *Config) { c.HTTPAddr = "" },
		"zero shutdown":      func(c *Config) { c.ShutdownTimeout = 0 },
		"bad log level":      func(c *Config) { c.Log.Level = "trace" },
		"bad log format":     func(c *Config) { c.Log.Format = "xml" },
		"bad sample ratio":   func(c *Config) { c.OTEL.SampleRatio = 2 },
		"min gt max conns": func(c *Config) {
			c.Database = DatabaseConfig{URL: "postgres://x", MinConns: 5, MaxConns: 2}
		},
		"prod without jwt": func(c *Config) { c.Environment = EnvProduction },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := baseValid()
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}

func TestLoad_DefaultsAndEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "0.25")

	cfg, err := Load("tenant")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ServiceName != "tenant" {
		t.Errorf("service name = %q", cfg.ServiceName)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level = %q", cfg.Log.Level)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("http addr = %q", cfg.HTTPAddr)
	}
	if cfg.OTEL.SampleRatio != 0.25 {
		t.Errorf("sample ratio = %v", cfg.OTEL.SampleRatio)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("shutdown default = %v", cfg.ShutdownTimeout)
	}
}
