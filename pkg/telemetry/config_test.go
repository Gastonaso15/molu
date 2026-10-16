package telemetry

import (
	"os"
	"testing"
)

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if cfg.Format != "json" {
		t.Errorf("Format default = %q, want json", cfg.Format)
	}
	if cfg.Level != "info" {
		t.Errorf("Level default = %q, want info", cfg.Level)
	}
	if cfg.OutputPath != "" {
		t.Errorf("OutputPath default = %q, want empty", cfg.OutputPath)
	}
	if cfg.Component != ComponentMoluFront {
		t.Errorf("Component default = %q, want %q", cfg.Component, ComponentMoluFront)
	}
	if cfg.MaxSizeMB != 100 {
		t.Errorf("MaxSizeMB default = %d, want 100", cfg.MaxSizeMB)
	}
	if cfg.MaxAgeDays != 7 {
		t.Errorf("MaxAgeDays default = %d, want 7", cfg.MaxAgeDays)
	}
	if !cfg.Compress {
		t.Error("Compress default = false, want true")
	}
	if cfg.SecurityMaxAgeDays != 90 {
		t.Errorf("SecurityMaxAgeDays default = %d, want 90", cfg.SecurityMaxAgeDays)
	}
	if cfg.Filename != "" {
		t.Errorf("Filename default = %q, want empty", cfg.Filename)
	}
}

func TestConfig_FromEnv(t *testing.T) {
	t.Parallel()

	// Set env vars
	os.Setenv("MOLU_FRONT_LOG_FORMAT", "console")
	os.Setenv("MOLU_FRONT_LOG_LEVEL", "debug")
	os.Setenv("MOLU_FRONT_LOG_OUTPUT_PATH", "/var/log/molu")
	os.Setenv("MOLU_FRONT_LOG_MAX_SIZE_MB", "50")
	os.Setenv("MOLU_FRONT_LOG_MAX_AGE_DAYS", "14")
	os.Setenv("MOLU_FRONT_LOG_COMPRESS", "false")
	os.Setenv("MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS", "180")
	os.Setenv("MOLU_FRONT_LOG_FILENAME", "custom.log")
	os.Setenv("MOLU_FRONT_TELEMETRY_PORT", "8080")
	os.Setenv("MOLU_FRONT_TELEMETRY_METRICS_PATH", "/custom-metrics")
	os.Setenv("MOLU_FRONT_TELEMETRY_HEALTH_PATH", "/custom-health")
	os.Setenv("MOLU_FRONT_TELEMETRY_ENABLED", "false")

	defer func() {
		os.Unsetenv("MOLU_FRONT_LOG_FORMAT")
		os.Unsetenv("MOLU_FRONT_LOG_LEVEL")
		os.Unsetenv("MOLU_FRONT_LOG_OUTPUT_PATH")
		os.Unsetenv("MOLU_FRONT_LOG_MAX_SIZE_MB")
		os.Unsetenv("MOLU_FRONT_LOG_MAX_AGE_DAYS")
		os.Unsetenv("MOLU_FRONT_LOG_COMPRESS")
		os.Unsetenv("MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS")
		os.Unsetenv("MOLU_FRONT_LOG_FILENAME")
		os.Unsetenv("MOLU_FRONT_TELEMETRY_PORT")
		os.Unsetenv("MOLU_FRONT_TELEMETRY_METRICS_PATH")
		os.Unsetenv("MOLU_FRONT_TELEMETRY_HEALTH_PATH")
		os.Unsetenv("MOLU_FRONT_TELEMETRY_ENABLED")
	}()

	cfg := LoadConfigFromEnv()

	if cfg.Format != "console" {
		t.Errorf("Format = %q, want console", cfg.Format)
	}
	if cfg.Level != "debug" {
		t.Errorf("Level = %q, want debug", cfg.Level)
	}
	if cfg.OutputPath != "/var/log/molu" {
		t.Errorf("OutputPath = %q, want /var/log/molu", cfg.OutputPath)
	}
	if cfg.MaxSizeMB != 50 {
		t.Errorf("MaxSizeMB = %d, want 50", cfg.MaxSizeMB)
	}
	if cfg.MaxAgeDays != 14 {
		t.Errorf("MaxAgeDays = %d, want 14", cfg.MaxAgeDays)
	}
	if cfg.Compress {
		t.Error("Compress = true, want false")
	}
	if cfg.SecurityMaxAgeDays != 180 {
		t.Errorf("SecurityMaxAgeDays = %d, want 180", cfg.SecurityMaxAgeDays)
	}
	if cfg.Filename != "custom.log" {
		t.Errorf("Filename = %q, want custom.log", cfg.Filename)
	}
	if cfg.TelemetryPort != 8080 {
		t.Errorf("TelemetryPort = %d, want 8080", cfg.TelemetryPort)
	}
	if cfg.TelemetryMetricsPath != "/custom-metrics" {
		t.Errorf("TelemetryMetricsPath = %q, want /custom-metrics", cfg.TelemetryMetricsPath)
	}
	if cfg.TelemetryHealthPath != "/custom-health" {
		t.Errorf("TelemetryHealthPath = %q, want /custom-health", cfg.TelemetryHealthPath)
	}
	if cfg.TelemetryEnabled {
		t.Error("TelemetryEnabled = true, want false")
	}
}

func TestConfig_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         Config
		wantError   bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Format:               "json",
				Level:                "info",
				OutputPath:           "",
				Component:            "test",
				MaxSizeMB:            100,
				MaxAgeDays:           7,
				Compress:             true,
				SecurityMaxAgeDays:   90,
				TelemetryPort:        9090,
				TelemetryMetricsPath: "/metrics",
				TelemetryHealthPath:  "/healthz",
				TelemetryEnabled:     true,
			},
			wantError: false,
		},
		{
			name: "invalid format",
			cfg: Config{
				Format:             "xml",
				Level:              "info",
				OutputPath:         "",
				Component:          "test",
				MaxSizeMB:          100,
				MaxAgeDays:         7,
				Compress:           true,
				SecurityMaxAgeDays: 90,
			},
			wantError: true,
		},
		{
			name: "invalid level",
			cfg: Config{
				Format:             "json",
				Level:              "trace",
				OutputPath:         "",
				Component:          "test",
				MaxSizeMB:          100,
				MaxAgeDays:         7,
				Compress:           true,
				SecurityMaxAgeDays: 90,
			},
			wantError: true,
		},
		{
			name: "negative max size",
			cfg: Config{
				Format:             "json",
				Level:              "info",
				OutputPath:         "",
				Component:          "test",
				MaxSizeMB:          -1,
				MaxAgeDays:         7,
				Compress:           true,
				SecurityMaxAgeDays: 90,
			},
			wantError: true,
		},
		{
			name: "negative max age",
			cfg: Config{
				Format:             "json",
				Level:              "info",
				OutputPath:         "",
				Component:          "test",
				MaxSizeMB:          100,
				MaxAgeDays:         -1,
				Compress:           true,
				SecurityMaxAgeDays: 90,
			},
			wantError: true,
		},
		{
			name: "empty component",
			cfg: Config{
				Format:             "json",
				Level:              "info",
				OutputPath:         "",
				Component:          "",
				MaxSizeMB:          100,
				MaxAgeDays:         7,
				Compress:           true,
				SecurityMaxAgeDays: 90,
			},
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfig_TelemetryDefaults(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if cfg.TelemetryPort != 9090 {
		t.Errorf("TelemetryPort default = %d, want 9090", cfg.TelemetryPort)
	}
	if cfg.TelemetryMetricsPath != "/metrics" {
		t.Errorf("TelemetryMetricsPath default = %q, want /metrics", cfg.TelemetryMetricsPath)
	}
	if cfg.TelemetryHealthPath != "/healthz" {
		t.Errorf("TelemetryHealthPath default = %q, want /healthz", cfg.TelemetryHealthPath)
	}
	if !cfg.TelemetryEnabled {
		t.Error("TelemetryEnabled default = false, want true")
	}
}