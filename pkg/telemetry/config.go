package telemetry

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// Config holds all telemetry configuration.
type Config struct {
	// Logging
	Format             string // "json" | "console"
	Level              string // "debug" | "info" | "warn" | "error"
	OutputPath         string // directory for log files, empty = stdout
	Component          string // component name (molu-front, hub-registry, etc.)
	MaxSizeMB          int    // max size per log file in MB
	MaxAgeDays         int    // max age in days
	Compress           bool   // compress rotated files
	SecurityMaxAgeDays int    // max age for security log in days
	Filename           string // base filename for log files (default: component name)

	// Telemetry server
	TelemetryPort        int    // port for /metrics and /healthz
	TelemetryMetricsPath string // path for metrics endpoint
	TelemetryHealthPath  string // path for health endpoint
	TelemetryEnabled     bool   // enable telemetry server
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Format:               "json",
		Level:                "info",
		OutputPath:           "",
		Component:            ComponentMoluFront,
		MaxSizeMB:            100,
		MaxAgeDays:           7,
		Compress:             true,
		SecurityMaxAgeDays:   90,
		Filename:             "",
		TelemetryPort:        9090,
		TelemetryMetricsPath: "/metrics",
		TelemetryHealthPath:  "/healthz",
		TelemetryEnabled:     true,
	}
}

// LoadConfigFromEnv loads configuration from environment variables.
func LoadConfigFromEnv() Config {
	cfg := DefaultConfig()

	if v := os.Getenv("MOLU_FRONT_LOG_FORMAT"); v != "" {
		cfg.Format = v
	}
	if v := os.Getenv("MOLU_FRONT_LOG_LEVEL"); v != "" {
		cfg.Level = v
	}
	if v := os.Getenv("MOLU_FRONT_LOG_OUTPUT_PATH"); v != "" {
		cfg.OutputPath = v
	}
	if v := os.Getenv("MOLU_FRONT_LOG_MAX_SIZE_MB"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.MaxSizeMB = val
		}
	}
	if v := os.Getenv("MOLU_FRONT_LOG_MAX_AGE_DAYS"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.MaxAgeDays = val
		}
	}
	if v := os.Getenv("MOLU_FRONT_LOG_COMPRESS"); v != "" {
		cfg.Compress = parseBool(v)
	}
	if v := os.Getenv("MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.SecurityMaxAgeDays = val
		}
	}
	if v := os.Getenv("MOLU_FRONT_LOG_FILENAME"); v != "" {
		cfg.Filename = v
	}
	if v := os.Getenv("MOLU_FRONT_TELEMETRY_PORT"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.TelemetryPort = val
		}
	}
	if v := os.Getenv("MOLU_FRONT_TELEMETRY_METRICS_PATH"); v != "" {
		cfg.TelemetryMetricsPath = v
	}
	if v := os.Getenv("MOLU_FRONT_TELEMETRY_HEALTH_PATH"); v != "" {
		cfg.TelemetryHealthPath = v
	}
	if v := os.Getenv("MOLU_FRONT_TELEMETRY_ENABLED"); v != "" {
		cfg.TelemetryEnabled = parseBool(v)
	}

	return cfg
}

// parseBool parses a boolean from string (true/false, 1/0, yes/no).
func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Validate format
	if c.Format != "json" && c.Format != "console" {
		return errors.New("invalid log format: must be 'json' or 'console'")
	}

	// Validate level
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[c.Level] {
		return errors.New("invalid log level: must be 'debug', 'info', 'warn', or 'error'")
	}

	// Validate component
	if c.Component == "" {
		return errors.New("component name cannot be empty")
	}

	// Validate max size
	if c.MaxSizeMB <= 0 {
		return errors.New("max size must be positive")
	}

	// Validate max age
	if c.MaxAgeDays < 0 {
		return errors.New("max age cannot be negative")
	}

	// Validate security max age
	if c.SecurityMaxAgeDays < 0 {
		return errors.New("security max age cannot be negative")
	}

	// Validate telemetry port
	if c.TelemetryPort <= 0 || c.TelemetryPort > 65535 {
		return errors.New("telemetry port must be between 1 and 65535")
	}

	// Validate telemetry paths
	if c.TelemetryMetricsPath == "" || c.TelemetryMetricsPath[0] != '/' {
		return errors.New("telemetry metrics path must start with /")
	}
	if c.TelemetryHealthPath == "" || c.TelemetryHealthPath[0] != '/' {
		return errors.New("telemetry health path must start with /")
	}

	return nil
}