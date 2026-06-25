// Package config provides gateway-specific configuration.
package config

import (
	"os"
	"strconv"
	"time"

	"github.com/bdsplatform/platform/backend/services/gateway/internal/proxy"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/router"
)

// GatewayConfig holds gateway-specific configuration.
type GatewayConfig struct {
	// Rate limiting.
	RateLimitRequestsPerMinute int
	RateLimitBurstSize         int

	// Backend services.
	Services router.Config
}

// Load loads gateway configuration from environment variables.
func Load() GatewayConfig {
	return GatewayConfig{
		RateLimitRequestsPerMinute: getInt("RATE_LIMIT_RPM", 60),
		RateLimitBurstSize:         getInt("RATE_LIMIT_BURST", 10),
		Services: router.Config{
			AuthService: proxy.ServiceConfig{
				Name:    "auth",
				BaseURL: getenv("AUTH_SERVICE_URL", "http://localhost:8081"),
				Timeout: getDuration("AUTH_SERVICE_TIMEOUT", 30*time.Second),
			},
			TenantService: proxy.ServiceConfig{
				Name:    "tenant",
				BaseURL: getenv("TENANT_SERVICE_URL", "http://localhost:8082"),
				Timeout: getDuration("TENANT_SERVICE_TIMEOUT", 30*time.Second),
			},
			ProjectService: proxy.ServiceConfig{
				Name:    "project",
				BaseURL: getenv("PROJECT_SERVICE_URL", "http://localhost:8083"),
				Timeout: getDuration("PROJECT_SERVICE_TIMEOUT", 30*time.Second),
			},
			AuditService: proxy.ServiceConfig{
				Name:    "audit",
				BaseURL: getenv("AUDIT_SERVICE_URL", "http://localhost:8084"),
				Timeout: getDuration("AUDIT_SERVICE_TIMEOUT", 30*time.Second),
			},
			// Future services.
			ClusterService: proxy.ServiceConfig{
				Name:    "cluster",
				BaseURL: getenv("CLUSTER_SERVICE_URL", ""),
				Timeout: getDuration("CLUSTER_SERVICE_TIMEOUT", 30*time.Second),
			},
			DeploymentService: proxy.ServiceConfig{
				Name:    "deployment",
				BaseURL: getenv("DEPLOYMENT_SERVICE_URL", ""),
				Timeout: getDuration("DEPLOYMENT_SERVICE_TIMEOUT", 60*time.Second),
			},
			SecretsService: proxy.ServiceConfig{
				Name:    "secrets",
				BaseURL: getenv("SECRETS_SERVICE_URL", ""),
				Timeout: getDuration("SECRETS_SERVICE_TIMEOUT", 30*time.Second),
			},
		},
	}
}

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

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
