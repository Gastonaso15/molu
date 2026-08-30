package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the complete configuration for the molu frontend process.
type Config struct {
	// Substrate (xolu) settings
	XoluURL            string
	XoluAuthMode       string
	XoluTokenFile      string
	XoluToken          string
	Tenant             string

	// Molu Hub settings
	HubURL             string
	HubAuthMode        string
	HubTokenFile       string
	HubToken           string

	// MCP Transport settings
	Transport          string // "stdio" or "streamable-http"
	HTTPAddr           string
	HTTPAuth           string // "none", "bearer", "mtls"
	HTTPBearerTokenFile string
	HTTPBearerToken    string

	// Refresh intervals
	SchemaPollInterval    time.Duration
	CataloguePollInterval time.Duration

	// xolu health probe settings
	PingInterval       time.Duration
	PingTimeout        time.Duration
	PongFreshness      time.Duration
	PingFailFloor      time.Duration
	PingFailCeiling    time.Duration
	StartupMaxAttempts int

	// Observability
	LogLevel           string
	LogFormat          string
	MetricsAddr        string

	// Execution Behaviour
	RedactPayloads     bool
	CallTimeout        time.Duration
}

// LoadFromEnv loads configuration parameters from environment variables with sensible defaults.
func LoadFromEnv() *Config {
	cfg := &Config{
		XoluURL:               getEnv("MOLU_FRONT_XOLU_URL", "http://localhost:8080"),
		XoluAuthMode:          getEnv("MOLU_FRONT_XOLU_AUTH_MODE", "bearer"),
		XoluTokenFile:         getEnv("MOLU_FRONT_XOLU_TOKEN_FILE", ""),
		XoluToken:             getEnv("MOLU_FRONT_XOLU_TOKEN", ""),
		Tenant:                getEnv("MOLU_FRONT_TENANT", "default"),

		HubURL:                getEnv("MOLU_HUB_URL", ""),
		HubAuthMode:           getEnv("MOLU_HUB_AUTH_MODE", "bearer"),
		HubTokenFile:          getEnv("MOLU_HUB_TOKEN_FILE", ""),
		HubToken:              getEnv("MOLU_HUB_TOKEN", ""),

		Transport:             getEnv("MOLU_FRONT_TRANSPORT", "stdio"),
		HTTPAddr:              getEnv("MOLU_FRONT_HTTP_ADDR", ":8090"),
		HTTPAuth:              getEnv("MOLU_FRONT_HTTP_AUTH", "none"),
		HTTPBearerTokenFile:   getEnv("MOLU_FRONT_HTTP_BEARER_TOKEN_FILE", ""),
		HTTPBearerToken:       getEnv("MOLU_FRONT_HTTP_BEARER_TOKEN", ""),

		SchemaPollInterval:    getDurationEnv("MOLU_FRONT_SCHEMA_POLL_INTERVAL", 60*time.Second),
		CataloguePollInterval: getDurationEnv("MOLU_FRONT_CATALOGUE_POLL_INTERVAL", 60*time.Second),

		PingInterval:          getDurationEnv("MOLU_FRONT_PING_INTERVAL", 30*time.Second),
		PingTimeout:           getDurationEnv("MOLU_FRONT_PING_TIMEOUT", 5*time.Second),
		PongFreshness:         getDurationEnv("MOLU_FRONT_PONG_FRESHNESS", 45*time.Second),
		PingFailFloor:         getDurationEnv("MOLU_FRONT_PING_FAIL_FLOOR", 1*time.Second),
		PingFailCeiling:       getDurationEnv("MOLU_FRONT_PING_FAIL_CEILING", 30*time.Second),
		StartupMaxAttempts:    getIntEnv("MOLU_FRONT_STARTUP_MAX_ATTEMPTS", 60),

		LogLevel:              getEnv("MOLU_FRONT_LOG_LEVEL", "info"),
		LogFormat:             getEnv("MOLU_FRONT_LOG_FORMAT", "console"),
		MetricsAddr:           getEnv("MOLU_FRONT_METRICS_ADDR", ":9090"),

		RedactPayloads:        getBoolEnv("MOLU_FRONT_REDACT_PAYLOADS", true),
		CallTimeout:           getDurationEnv("MOLU_FRONT_CALL_TIMEOUT", 30*time.Second),
	}

	// Read token files if set
	if cfg.XoluTokenFile != "" && cfg.XoluToken == "" {
		if data, err := os.ReadFile(cfg.XoluTokenFile); err == nil {
			cfg.XoluToken = strings.TrimSpace(string(data))
		}
	}
	if cfg.HubTokenFile != "" && cfg.HubToken == "" {
		if data, err := os.ReadFile(cfg.HubTokenFile); err == nil {
			cfg.HubToken = strings.TrimSpace(string(data))
		}
	}
	if cfg.HTTPBearerTokenFile != "" && cfg.HTTPBearerToken == "" {
		if data, err := os.ReadFile(cfg.HTTPBearerTokenFile); err == nil {
			cfg.HTTPBearerToken = strings.TrimSpace(string(data))
		}
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getDurationEnv(key string, defaultVal time.Duration) time.Duration {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(valStr)
	if err != nil {
		return defaultVal
	}
	return d
}

func getIntEnv(key string, defaultVal int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return n
}

func getBoolEnv(key string, defaultVal bool) bool {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(valStr)
	if err != nil {
		return defaultVal
	}
	return b
}
