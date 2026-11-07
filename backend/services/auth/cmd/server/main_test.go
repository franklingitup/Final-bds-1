package main

import (
	"testing"

	"github.com/bdsplatform/platform/backend/libs/config"
)

func TestRedisRequired(t *testing.T) {
	for _, environment := range []config.Environment{config.EnvProduction, config.EnvStaging} {
		if !redisRequired(config.Config{Environment: environment}) {
			t.Errorf("REDIS_URL should be required in %s", environment)
		}
	}
	if redisRequired(config.Config{Environment: config.EnvDevelopment}) {
		t.Error("REDIS_URL should remain optional in development")
	}
}
