package telemetry

import (
	"context"
	"time"
)

// LogDomain represents the operational domain of a log entry.
type LogDomain string

const (
	DomainSystem     LogDomain = "system"
	DomainApplication LogDomain = "application"
	DomainSecurity   LogDomain = "security"
	DomainNetwork    LogDomain = "network"
)

// LogLevel represents the severity level of a log entry.
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
)

// LogEntry represents a structured log entry with all base fields.
type LogEntry struct {
	Timestamp       string         `json:"timestamp"`
	Level           string         `json:"level"`
	Domain          string         `json:"domain"`
	Component       string         `json:"component"`
	RequestID       string         `json:"request_id,omitempty"`
	TraceID         string         `json:"trace_id,omitempty"`
	SpanID          string         `json:"span_id,omitempty"`
	Message         string         `json:"message"`
	Fields          map[string]any `json:"fields,omitempty"`
	// Security-specific fields
	Actor    string `json:"actor,omitempty"`
	Action   string `json:"action,omitempty"`
	Resource string `json:"resource,omitempty"`
	Decision string `json:"decision,omitempty"` // ALLOW/DENY
	Reason   string `json:"reason,omitempty"`
}

// RequestContext holds correlation IDs and request metadata propagated via context.Context.
type RequestContext struct {
	RequestID string
	TraceID   string
	SpanID    string
	TenantID  string
	UserID    string
	Method    string
	Tool      string
}

// Context keys for RequestContext propagation.
type contextKey string

const (
	RequestContextKey contextKey = "telemetry_request_context"
)

// WithRequestContext returns a new context with the RequestContext embedded.
func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, RequestContextKey, rc)
}

// RequestContextFrom extracts the RequestContext from context, or returns a new empty one.
func RequestContextFrom(ctx context.Context) *RequestContext {
	if rc, ok := ctx.Value(RequestContextKey).(*RequestContext); ok {
		return rc
	}
	return &RequestContext{}
}

// RetryMetadata holds retry information for SUBSTRATE_UNAVAILABLE errors.
type RetryMetadata struct {
	LastSuccessfulContact string `json:"lastSuccessfulContact"` // RFC3339
	NextRetryAt           string `json:"nextRetryAt"`           // RFC3339
	AttemptNumber         int    `json:"attemptNumber"`
	MaxAttempts           int    `json:"maxAttempts"`
}

// Component names used in logs.
const (
	ComponentMoluFront     = "molu-front"
	ComponentHubRegistry   = "hub-registry"
	ComponentXoluClient    = "xolu-client"
	ComponentRPIProvider   = "rpi-provider"
	ComponentMCPTransport  = "mcp-transport"
)

// Security decision constants.
const (
	DecisionAllow = "ALLOW"
	DecisionDeny  = "DENY"
)

// NowFunc allows tests to control time.
var NowFunc = time.Now

// RFC3339Nano returns current time in RFC3339 with nanosecond precision.
func RFC3339Nano() string {
	return NowFunc().Format(time.RFC3339Nano)
}