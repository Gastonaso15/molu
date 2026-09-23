package config

import (
	"os"
	"time"
)

type Config struct {
	XoluURL     string
	Tenant      string
	Transport   string
	CallTimeout time.Duration
	LogLevel    string
}

func LoadFromEnv() *Config {
	return &Config{
		XoluURL:     getEnv("MOLU_FRONT_XOLU_URL", "https://localhost:8080"),
		Tenant:      getEnv("MOLU_FRONT_TENANT", "default"),
		Transport:   getEnv("MOLU_FRONT_TRANSPORT", "stdio"),
		CallTimeout: 30 * time.Second,
		LogLevel:    getEnv("MOLU_FRONT_LOG_LEVEL", "info"),
	}
}

func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
