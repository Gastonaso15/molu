package config

import (
	"strings"
	"testing"
	"time"
)

// setRequired fija las tres variables obligatorias con valores válidos.
func setRequired(t *testing.T) {
	t.Setenv("MOLU_FRONT_XOLU_URL", "http://localhost:8080")
	t.Setenv("MOLU_FRONT_XOLU_AUTH_MODE", "jwt")
	t.Setenv("MOLU_FRONT_XOLU_TOKEN_FILE", "xolu.jwt")
}

func TestDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if cfg.Transport != "stdio" {
		t.Errorf("Transport = %q, se esperaba stdio", cfg.Transport)
	}
	if cfg.HTTPAddr != ":8090" {
		t.Errorf("HTTPAddr = %q, se esperaba :8090", cfg.HTTPAddr)
	}
	if cfg.SchemaPollInterval != 60*time.Second {
		t.Errorf("SchemaPollInterval = %v, se esperaba 60s", cfg.SchemaPollInterval)
	}
	if cfg.SchemaRefreshInterval != 60*time.Second {
		t.Errorf("SchemaRefreshInterval = %v, se esperaba 60s", cfg.SchemaRefreshInterval)
	}
	if cfg.SchemaRetryFloor != 1*time.Second {
		t.Errorf("SchemaRetryFloor = %v, se esperaba 1s", cfg.SchemaRetryFloor)
	}
	if cfg.SchemaRetryCeiling != 30*time.Second {
		t.Errorf("SchemaRetryCeiling = %v, se esperaba 30s", cfg.SchemaRetryCeiling)
	}
	if cfg.SchemaMaxAttempts != 0 {
		t.Errorf("SchemaMaxAttempts = %d, se esperaba 0 (ilimitado)", cfg.SchemaMaxAttempts)
	}
	if cfg.SchemaTimeout != 10*time.Second {
		t.Errorf("SchemaTimeout = %v, se esperaba 10s", cfg.SchemaTimeout)
	}
	if cfg.PingInterval != 30*time.Second {
		t.Errorf("PingInterval = %v, se esperaba 30s", cfg.PingInterval)
	}
	if cfg.PongFreshness != 45*time.Second {
		t.Errorf("PongFreshness = %v, se esperaba 45s", cfg.PongFreshness)
	}
	if cfg.StartupMaxAttempts != 60 {
		t.Errorf("StartupMaxAttempts = %d, se esperaba 60", cfg.StartupMaxAttempts)
	}
	if !cfg.RedactPayloads {
		t.Error("RedactPayloads debería ser true por defecto")
	}
	if cfg.HubURL != "" {
		t.Errorf("HubURL = %q, se esperaba vacío", cfg.HubURL)
	}
}

func TestFaltanObligatorias(t *testing.T) {
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("se esperaba un error por variables obligatorias faltantes")
	}
	for _, name := range []string{"MOLU_FRONT_XOLU_URL", "MOLU_FRONT_XOLU_AUTH_MODE", "MOLU_FRONT_XOLU_TOKEN_FILE"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("el error no menciona %s: %v", name, err)
		}
	}
}

func TestValoresInvalidos(t *testing.T) {
	setRequired(t)
	t.Setenv("MOLU_FRONT_TRANSPORT", "sse")
	t.Setenv("MOLU_FRONT_PING_INTERVAL", "treinta")
	t.Setenv("MOLU_FRONT_SCHEMA_RETRY_FLOOR", "5s")
	t.Setenv("MOLU_FRONT_SCHEMA_RETRY_CEILING", "2s")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	if !strings.Contains(err.Error(), "MOLU_FRONT_TRANSPORT") {
		t.Errorf("el error no menciona MOLU_FRONT_TRANSPORT: %v", err)
	}
	if !strings.Contains(err.Error(), "MOLU_FRONT_PING_INTERVAL") {
		t.Errorf("el error no menciona MOLU_FRONT_PING_INTERVAL: %v", err)
	}
	if !strings.Contains(err.Error(), "MOLU_FRONT_SCHEMA_RETRY_FLOOR") {
		t.Errorf("el error no menciona MOLU_FRONT_SCHEMA_RETRY_FLOOR: %v", err)
	}
}

func TestHubRequiereCredencial(t *testing.T) {
	setRequired(t)
	t.Setenv("MOLU_HUB_URL", "http://localhost:8091")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("se esperaba un error porque falta la credencial del hub")
	}
	if !strings.Contains(err.Error(), "MOLU_HUB_TOKEN_FILE") {
		t.Errorf("el error no menciona MOLU_HUB_TOKEN_FILE: %v", err)
	}
}