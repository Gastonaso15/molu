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