// Package config carga la configuración de molu desde variables de entorno.
// Las variables y sus valores por defecto son los de la especificación,
// Parte 2 §11.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// xolu (obligatorias)
	XoluURL       string // MOLU_FRONT_XOLU_URL
	XoluAuthMode  string // MOLU_FRONT_XOLU_AUTH_MODE: bearer | apikey | jwt
	XoluTokenFile string // MOLU_FRONT_XOLU_TOKEN_FILE

	// Hub (opcional: si HubURL está vacío no hay funciones de dominio)
	HubURL       string // MOLU_HUB_URL
	HubAuthMode  string // MOLU_HUB_AUTH_MODE
	HubTokenFile string // MOLU_HUB_TOKEN_FILE

	// Transporte MCP
	Transport           string // MOLU_FRONT_TRANSPORT: stdio | streamable-http
	HTTPAddr            string // MOLU_FRONT_HTTP_ADDR
	HTTPAuth            string // MOLU_FRONT_HTTP_AUTH: none | bearer | mtls
	HTTPBearerTokenFile string // MOLU_FRONT_HTTP_BEARER_TOKEN_FILE

	// Refresco
	SchemaPollInterval    time.Duration // MOLU_FRONT_SCHEMA_POLL_INTERVAL
	CataloguePollInterval time.Duration // MOLU_FRONT_CATALOGUE_POLL_INTERVAL

	// Schema Reader (Spec 003)
	SchemaRefreshInterval time.Duration // MOLU_FRONT_SCHEMA_REFRESH_INTERVAL
	SchemaRetryFloor      time.Duration // MOLU_FRONT_SCHEMA_RETRY_FLOOR
	SchemaRetryCeiling    time.Duration // MOLU_FRONT_SCHEMA_RETRY_CEILING
	SchemaMaxAttempts     int           // MOLU_FRONT_SCHEMA_MAX_ATTEMPTS (0 = ilimitado)
	SchemaTimeout         time.Duration // MOLU_FRONT_SCHEMA_TIMEOUT

	// Sonda de salud de xolu (§8)
	PingInterval       time.Duration // MOLU_FRONT_PING_INTERVAL
	PingTimeout        time.Duration // MOLU_FRONT_PING_TIMEOUT
	PongFreshness      time.Duration // MOLU_FRONT_PONG_FRESHNESS
	PingFailFloor      time.Duration // MOLU_FRONT_PING_FAIL_FLOOR
	PingFailCeiling    time.Duration // MOLU_FRONT_PING_FAIL_CEILING
	StartupMaxAttempts int           // MOLU_FRONT_STARTUP_MAX_ATTEMPTS (0 = ilimitado)

	// Observabilidad
	LogLevel    string // MOLU_FRONT_LOG_LEVEL: debug | info | warn | error
	LogFormat   string // MOLU_FRONT_LOG_FORMAT: console | json
	MetricsAddr string // MOLU_FRONT_METRICS_ADDR (vacío = deshabilitado)

	// Comportamiento
	RedactPayloads bool          // MOLU_FRONT_REDACT_PAYLOADS
	CallTimeout    time.Duration // MOLU_FRONT_CALL_TIMEOUT
}

// LoadFromEnv lee la configuración de las variables de entorno y la valida.
// Devuelve todos los problemas encontrados juntos, no solo el primero.
func LoadFromEnv() (*Config, error) {
	r := &reader{}

	cfg := &Config{
		XoluURL:       r.str("MOLU_FRONT_XOLU_URL", ""),
		XoluAuthMode:  r.str("MOLU_FRONT_XOLU_AUTH_MODE", ""),
		XoluTokenFile: r.str("MOLU_FRONT_XOLU_TOKEN_FILE", ""),

		HubURL:       r.str("MOLU_HUB_URL", ""),
		HubAuthMode:  r.str("MOLU_HUB_AUTH_MODE", ""),
		HubTokenFile: r.str("MOLU_HUB_TOKEN_FILE", ""),

		Transport:           r.str("MOLU_FRONT_TRANSPORT", "stdio"),
		HTTPAddr:            r.str("MOLU_FRONT_HTTP_ADDR", ":8090"),
		HTTPAuth:            r.str("MOLU_FRONT_HTTP_AUTH", "none"),
		HTTPBearerTokenFile: r.str("MOLU_FRONT_HTTP_BEARER_TOKEN_FILE", ""),

		SchemaPollInterval:    r.duration("MOLU_FRONT_SCHEMA_POLL_INTERVAL", 60*time.Second),
		CataloguePollInterval: r.duration("MOLU_FRONT_CATALOGUE_POLL_INTERVAL", 60*time.Second),

		SchemaRefreshInterval: r.duration("MOLU_FRONT_SCHEMA_REFRESH_INTERVAL", 60*time.Second),
		SchemaRetryFloor:      r.duration("MOLU_FRONT_SCHEMA_RETRY_FLOOR", 1*time.Second),
		SchemaRetryCeiling:    r.duration("MOLU_FRONT_SCHEMA_RETRY_CEILING", 30*time.Second),
		SchemaMaxAttempts:     r.integer("MOLU_FRONT_SCHEMA_MAX_ATTEMPTS", 0),
		SchemaTimeout:         r.duration("MOLU_FRONT_SCHEMA_TIMEOUT", 10*time.Second),

		PingInterval:       r.duration("MOLU_FRONT_PING_INTERVAL", 30*time.Second),
		PingTimeout:        r.duration("MOLU_FRONT_PING_TIMEOUT", 5*time.Second),
		PongFreshness:      r.duration("MOLU_FRONT_PONG_FRESHNESS", 45*time.Second),
		PingFailFloor:      r.duration("MOLU_FRONT_PING_FAIL_FLOOR", 1*time.Second),
		PingFailCeiling:    r.duration("MOLU_FRONT_PING_FAIL_CEILING", 30*time.Second),
		StartupMaxAttempts: r.integer("MOLU_FRONT_STARTUP_MAX_ATTEMPTS", 60),

		LogLevel:    r.str("MOLU_FRONT_LOG_LEVEL", "info"),
		LogFormat:   r.str("MOLU_FRONT_LOG_FORMAT", "console"),
		MetricsAddr: r.str("MOLU_FRONT_METRICS_ADDR", ":9090"),

		RedactPayloads: r.boolean("MOLU_FRONT_REDACT_PAYLOADS", true),
		CallTimeout:    r.duration("MOLU_FRONT_CALL_TIMEOUT", 30*time.Second),
	}

	r.errs = append(r.errs, cfg.validate()...)
	if len(r.errs) > 0 {
		return nil, errors.Join(r.errs...)
	}
	return cfg, nil
}

func (c *Config) validate() []error {
	var errs []error
	required := func(name, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s es obligatoria", name))
		}
	}
	oneOf := func(name, value string, allowed ...string) {
		for _, a := range allowed {
			if value == a {
				return
			}
		}
		errs = append(errs, fmt.Errorf("%s=%q no es válido (opciones: %v)", name, value, allowed))
	}

	// xolu
	required("MOLU_FRONT_XOLU_URL", c.XoluURL)
	required("MOLU_FRONT_XOLU_TOKEN_FILE", c.XoluTokenFile)
	oneOf("MOLU_FRONT_XOLU_AUTH_MODE", c.XoluAuthMode, "bearer", "apikey", "jwt")

	// Hub: solo se valida si está configurado
	if c.HubURL != "" {
		oneOf("MOLU_HUB_AUTH_MODE", c.HubAuthMode, "bearer", "apikey", "jwt")
		required("MOLU_HUB_TOKEN_FILE", c.HubTokenFile)
	}

	// Transporte MCP
	oneOf("MOLU_FRONT_TRANSPORT", c.Transport, "stdio", "streamable-http")
	oneOf("MOLU_FRONT_HTTP_AUTH", c.HTTPAuth, "none", "bearer", "mtls")
	if c.HTTPAuth == "bearer" {
		required("MOLU_FRONT_HTTP_BEARER_TOKEN_FILE", c.HTTPBearerTokenFile)
	}

	// Observabilidad
	oneOf("MOLU_FRONT_LOG_LEVEL", c.LogLevel, "debug", "info", "warn", "error")
	oneOf("MOLU_FRONT_LOG_FORMAT", c.LogFormat, "console", "json")

	// Tiempos
	positive := func(name string, d time.Duration) {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("%s debe ser mayor que cero", name))
		}
	}
	positive("MOLU_FRONT_SCHEMA_POLL_INTERVAL", c.SchemaPollInterval)
	positive("MOLU_FRONT_CATALOGUE_POLL_INTERVAL", c.CataloguePollInterval)
	positive("MOLU_FRONT_SCHEMA_REFRESH_INTERVAL", c.SchemaRefreshInterval)
	positive("MOLU_FRONT_SCHEMA_RETRY_FLOOR", c.SchemaRetryFloor)
	positive("MOLU_FRONT_SCHEMA_RETRY_CEILING", c.SchemaRetryCeiling)
	positive("MOLU_FRONT_SCHEMA_TIMEOUT", c.SchemaTimeout)
	if c.SchemaRetryFloor > c.SchemaRetryCeiling {
		errs = append(errs, errors.New("MOLU_FRONT_SCHEMA_RETRY_FLOOR no puede ser mayor que MOLU_FRONT_SCHEMA_RETRY_CEILING"))
	}
	if c.SchemaMaxAttempts < 0 {
		errs = append(errs, errors.New("MOLU_FRONT_SCHEMA_MAX_ATTEMPTS no puede ser negativo (0 = ilimitado)"))
	}
	positive("MOLU_FRONT_PING_INTERVAL", c.PingInterval)
	positive("MOLU_FRONT_PING_TIMEOUT", c.PingTimeout)
	positive("MOLU_FRONT_PONG_FRESHNESS", c.PongFreshness)
	positive("MOLU_FRONT_PING_FAIL_FLOOR", c.PingFailFloor)
	positive("MOLU_FRONT_PING_FAIL_CEILING", c.PingFailCeiling)
	positive("MOLU_FRONT_CALL_TIMEOUT", c.CallTimeout)
	if c.PingFailFloor > c.PingFailCeiling {
		errs = append(errs, errors.New("MOLU_FRONT_PING_FAIL_FLOOR no puede ser mayor que MOLU_FRONT_PING_FAIL_CEILING"))
	}
	if c.StartupMaxAttempts < 0 {
		errs = append(errs, errors.New("MOLU_FRONT_STARTUP_MAX_ATTEMPTS no puede ser negativo (0 = ilimitado)"))
	}

	return errs
}

// reader lee variables de entorno y junta los errores de formato.
type reader struct {
	errs []error
}

func (r *reader) str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s=%q no es una duración válida (ejemplo: 30s, 1m)", key, v))
		return def
	}
	return d
}

func (r *reader) integer(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s=%q no es un número entero", key, v))
		return def
	}
	return n
}

func (r *reader) boolean(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s=%q no es true ni false", key, v))
		return def
	}
	return b
}