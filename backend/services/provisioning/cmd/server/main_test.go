package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/config"
	provisioning "github.com/bdsplatform/platform/backend/services/provisioning/internal"
)

func TestInitEncryptor(t *testing.T) {
	// Generate a valid 32-byte key for testing
	validKey := base64.StdEncoding.EncodeToString(make([]byte, 32))

	tests := []struct {
		name        string
		key         string
		env         config.Environment
		wantErr     bool
		errContains string
		wantWarning bool
		wantNoop    bool
	}{
		{
			name:    "valid key in production",
			key:     validKey,
			env:     config.EnvProduction,
			wantErr: false,
		},
		{
			name:    "valid key in staging",
			key:     validKey,
			env:     config.EnvStaging,
			wantErr: false,
		},
		{
			name:    "valid key in development",
			key:     validKey,
			env:     config.EnvDevelopment,
			wantErr: false,
		},
		{
			name:        "unset key in production fails",
			key:         "",
			env:         config.EnvProduction,
			wantErr:     true,
			errContains: "required outside local development",
		},
		{
			name:        "unset key in staging fails",
			key:         "",
			env:         config.EnvStaging,
			wantErr:     true,
			errContains: "required outside local development",
		},
		{
			name:        "unset key in development warns and proceeds",
			key:         "",
			env:         config.EnvDevelopment,
			wantErr:     false,
			wantWarning: true,
			wantNoop:    true,
		},
		{
			name:        "invalid base64 fails in production",
			key:         "not-valid-base64!!!",
			env:         config.EnvProduction,
			wantErr:     true,
			errContains: "not valid base64",
		},
		{
			name:        "invalid base64 fails in development",
			key:         "not-valid-base64!!!",
			env:         config.EnvDevelopment,
			wantErr:     true,
			errContains: "not valid base64",
		},
		{
			name:        "wrong length key fails in production",
			key:         base64.StdEncoding.EncodeToString([]byte("too-short")),
			env:         config.EnvProduction,
			wantErr:     true,
			errContains: "must be exactly 32 bytes",
		},
		{
			name:        "wrong length key fails in development",
			key:         base64.StdEncoding.EncodeToString([]byte("too-short")),
			env:         config.EnvDevelopment,
			wantErr:     true,
			errContains: "must be exactly 32 bytes",
		},
		{
			name:        "16-byte key fails (AES-128 not allowed)",
			key:         base64.StdEncoding.EncodeToString(make([]byte, 16)),
			env:         config.EnvProduction,
			wantErr:     true,
			errContains: "must be exactly 32 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := initEncryptor(tt.key, tt.env)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.encryptor == nil {
				t.Fatal("expected non-nil encryptor")
			}

			if tt.wantWarning && result.warning == "" {
				t.Error("expected warning message, got empty string")
			}
			if !tt.wantWarning && result.warning != "" {
				t.Errorf("unexpected warning: %s", result.warning)
			}

			_, isNoop := result.encryptor.(*provisioning.NoopEncryptor)
			if tt.wantNoop && !isNoop {
				t.Error("expected NoopEncryptor")
			}
			if !tt.wantNoop && isNoop {
				t.Error("did not expect NoopEncryptor")
			}
		})
	}
}
