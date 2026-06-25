// Package config loads and validates service configuration from the environment.
//
// Configuration is read once at startup via Load/MustLoad and validated before
// the service begins serving traffic. Defaults target local development; every
// value can be overridden through environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment enumerates supported deployment environments.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// Config is the fully-resolved configuration for a single service.
type Config struct {
	ServiceName     string
	Environment     Environment
	HTTPAddr        string
	ShutdownTimeout time.Duration

	Log      LogConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	OTEL     OTELConfig
	Auth     AuthConfig
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// DatabaseConfig controls the PostgreSQL connection pool.
type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// RedisConfig controls the Redis connection.
type RedisConfig struct {
	URL string
}

// NATSConfig controls the NATS/JetStream event backbone (libs/events).
type NATSConfig struct {
	URL string // e.g. nats://localhost:4222. Empty disables eventing.
	// Stream is the JetStream stream capturing all platform events
	// (subjects "<SubjectPrefix>.>").
	Stream string
	// DLQStream captures dead-lettered events (subjects "dlq.>").
	DLQStream string
	// SubjectPrefix namespaces event subjects, e.g. "evt".
	SubjectPrefix string
}

// OTELConfig controls OpenTelemetry tracing.
type OTELConfig struct {
	Endpoint    string  // OTLP gRPC endpoint, e.g. http://localhost:4317. Empty disables export.
	Insecure    bool    // use plaintext gRPC
	SampleRatio float64 // 0.0 - 1.0
}

// AuthConfig controls token issuance and validation.
type AuthConfig struct {
	JWTSigningKey string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
}

// Load resolves configuration for the named service and validates it.
func Load(serviceName string) (Config, error) {
	cfg := Config{
		ServiceName:     serviceName,
		Environment:     Environment(getenv("ENVIRONMENT", string(EnvDevelopment))),
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		ShutdownTimeout: getDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		Log: LogConfig{
			Level:  getenv("LOG_LEVEL", "info"),
			Format: getenv("LOG_FORMAT", "json"),
		},
		Database: DatabaseConfig{
			URL:             getenv("DATABASE_URL", ""),
			MaxConns:        int32(getInt("DATABASE_MAX_CONNS", 10)),
			MinConns:        int32(getInt("DATABASE_MIN_CONNS", 2)),
			MaxConnLifetime: getDuration("DATABASE_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getDuration("DATABASE_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
		Redis: RedisConfig{
			URL: getenv("REDIS_URL", ""),
		},
		NATS: NATSConfig{
			URL:           getenv("NATS_URL", ""),
			Stream:        getenv("NATS_STREAM", "EVENTS"),
			DLQStream:     getenv("NATS_DLQ_STREAM", "EVENTS_DLQ"),
			SubjectPrefix: getenv("NATS_SUBJECT_PREFIX", "evt"),
		},
		OTEL: OTELConfig{
			Endpoint:    getenv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			Insecure:    getBool("OTEL_EXPORTER_OTLP_INSECURE", true),
			SampleRatio: getFloat("OTEL_TRACES_SAMPLER_RATIO", 1.0),
		},
		Auth: AuthConfig{
			JWTSigningKey: getenv("JWT_SIGNING_KEY", ""),
			AccessTTL:     getDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTTL:    getDuration("REFRESH_TOKEN_TTL", 720*time.Hour),
		},
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// MustLoad is like Load but panics on error. Intended for use in main().
func MustLoad(serviceName string) Config {
	cfg, err := Load(serviceName)
	if err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
	return cfg
}

// Validate verifies the configuration is internally consistent and usable.
func (c Config) Validate() error {
	var errs []string

	if strings.TrimSpace(c.ServiceName) == "" {
		errs = append(errs, "service name is required")
	}
	switch c.Environment {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Sprintf("invalid environment %q", c.Environment))
	}
	if c.HTTPAddr == "" {
		errs = append(errs, "HTTP_ADDR is required")
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, "SHUTDOWN_TIMEOUT must be positive")
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("invalid LOG_LEVEL %q", c.Log.Level))
	}
	switch c.Log.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Sprintf("invalid LOG_FORMAT %q", c.Log.Format))
	}

	if c.Database.URL != "" {
		if _, err := url.Parse(c.Database.URL); err != nil {
			errs = append(errs, fmt.Sprintf("invalid DATABASE_URL: %v", err))
		}
		if c.Database.MinConns < 0 || c.Database.MaxConns < 1 {
			errs = append(errs, "database pool sizes must be >= 0 (min) and >= 1 (max)")
		}
		if c.Database.MinConns > c.Database.MaxConns {
			errs = append(errs, "DATABASE_MIN_CONNS cannot exceed DATABASE_MAX_CONNS")
		}
	}

	if c.Redis.URL != "" {
		if _, err := url.Parse(c.Redis.URL); err != nil {
			errs = append(errs, fmt.Sprintf("invalid REDIS_URL: %v", err))
		}
	}

	if c.NATS.URL != "" {
		if _, err := url.Parse(c.NATS.URL); err != nil {
			errs = append(errs, fmt.Sprintf("invalid NATS_URL: %v", err))
		}
		if c.NATS.Stream == "" || c.NATS.DLQStream == "" {
			errs = append(errs, "NATS_STREAM and NATS_DLQ_STREAM are required when NATS_URL is set")
		}
		if c.NATS.SubjectPrefix == "" {
			errs = append(errs, "NATS_SUBJECT_PREFIX is required when NATS_URL is set")
		}
	}

	if c.OTEL.SampleRatio < 0 || c.OTEL.SampleRatio > 1 {
		errs = append(errs, "OTEL_TRACES_SAMPLER_RATIO must be between 0 and 1")
	}

	// In production, secrets must not be left at their empty defaults.
	if c.Environment == EnvProduction && c.Auth.JWTSigningKey == "" {
		errs = append(errs, "JWT_SIGNING_KEY is required in production")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(errs, "; "))
	}
	return nil
}

// IsProduction reports whether the service runs in the production environment.
func (c Config) IsProduction() bool { return c.Environment == EnvProduction }

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getFloat(key string, fallback float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
