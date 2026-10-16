package telemetry

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

// Logger interface for structured logging across domains.
type Logger interface {
	System()     *zerolog.Logger
	Application() *zerolog.Logger
	Security()   *zerolog.Logger
	Network()    *zerolog.Logger
	WithContext(ctx context.Context) Logger
	WithFields(fields map[string]any) Logger
	Level() LogLevel
	SetLevel(level LogLevel)
}

// zerologLogger implements Logger using zerolog.
type zerologLogger struct {
	mu           sync.RWMutex
	system       *zerolog.Logger
	application  *zerolog.Logger
	security     *zerolog.Logger
	network      *zerolog.Logger
	level        LogLevel
	component    string
	securityFile *os.File
}

// NewLogger creates a new multi-domain logger.
func NewLogger(cfg Config, extraWriters ...io.Writer) Logger {
	// Parse level
	lvl := parseLevel(cfg.Level)

	// Configure zerolog
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "timestamp"
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.TimestampFunc = func() time.Time { return NowFunc() }

	// Base logger with component and timestamp
	base := zerolog.New(io.Discard).With().
		Str("component", cfg.Component).
		Timestamp().
		Logger()

	// Determine output writers for each domain
	var systemOut, appOut, networkOut io.Writer
	var securityOut io.Writer

	if cfg.OutputPath != "" {
		// File-based logging with rotation
		systemOut = newRotatingWriter(cfg.OutputPath, "system.log", cfg.MaxSizeMB, cfg.MaxAgeDays, cfg.Compress)
		appOut = newRotatingWriter(cfg.OutputPath, "application.log", cfg.MaxSizeMB, cfg.MaxAgeDays, cfg.Compress)
		networkOut = newRotatingWriter(cfg.OutputPath, "network.log", cfg.MaxSizeMB, cfg.MaxAgeDays, cfg.Compress)

		// Security log: separate file, sync write, extended retention
		secFile := newSecurityWriter(cfg.OutputPath, "security.log", cfg.SecurityMaxAgeDays)
		securityOut = secFile
	} else {
		// Console/stderr logging (stdio MCP safe)
		var out io.Writer
		if cfg.Format == "json" {
			out = os.Stderr
		} else {
			out = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		}
		systemOut = out
		appOut = out
		networkOut = out
		securityOut = out
	}

	// Add extra writers (for testing)
	for _, w := range extraWriters {
		systemOut = io.MultiWriter(systemOut, w)
		appOut = io.MultiWriter(appOut, w)
		networkOut = io.MultiWriter(networkOut, w)
		securityOut = io.MultiWriter(securityOut, w)
	}

	// Create domain loggers with domain field (allocate on heap)
	sysLogger := base.Output(systemOut).Level(toZerologLevel(lvl)).With().Str("domain", string(DomainSystem)).Logger()
	appLogger := base.Output(appOut).Level(toZerologLevel(lvl)).With().Str("domain", string(DomainApplication)).Logger()
	secLogger := base.Output(securityOut).Level(toZerologLevel(lvl)).With().Str("domain", string(DomainSecurity)).Logger()
	netLogger := base.Output(networkOut).Level(toZerologLevel(lvl)).With().Str("domain", string(DomainNetwork)).Logger()

	l := &zerologLogger{
		system:      &sysLogger,
		application: &appLogger,
		security:    &secLogger,
		network:     &netLogger,
		level:       lvl,
		component:   cfg.Component,
	}

	if cfg.OutputPath != "" {
		// Store security file handle for potential cleanup
		if sf, ok := securityOut.(*os.File); ok {
			l.securityFile = sf
		}
	}

	return l
}

func (l *zerologLogger) System() *zerolog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.system
}

func (l *zerologLogger) Application() *zerolog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.application
}

func (l *zerologLogger) Security() *zerolog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.security
}

func (l *zerologLogger) Network() *zerolog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.network
}

func (l *zerologLogger) WithContext(ctx context.Context) Logger {
	rc := RequestContextFrom(ctx)
	if rc.RequestID == "" && rc.TraceID == "" && rc.SpanID == "" {
		return l
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	return &contextLogger{
		Logger: l,
		rc:     rc,
	}
}

func (l *zerologLogger) WithFields(fields map[string]any) Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return &fieldsLogger{
		Logger: l,
		fields: fields,
	}
}

func (l *zerologLogger) Level() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

func (l *zerologLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
	zlvl := toZerologLevel(level)
	sys := l.system.Level(zlvl)
	app := l.application.Level(zlvl)
	sec := l.security.Level(zlvl)
	net := l.network.Level(zlvl)
	l.system = &sys
	l.application = &app
	l.security = &sec
	l.network = &net
}

// contextLogger wraps Logger to add request context fields.
type contextLogger struct {
	Logger
	rc *RequestContext
}

func (cl *contextLogger) System() *zerolog.Logger {
	l := cl.Logger.System()
	with := l.With().
		Str("request_id", cl.rc.RequestID).
		Str("trace_id", cl.rc.TraceID).
		Str("span_id", cl.rc.SpanID).
		Str("tenant_id", cl.rc.TenantID).
		Str("user_id", cl.rc.UserID).
		Str("method", cl.rc.Method).
		Str("tool", cl.rc.Tool)
	logger := with.Logger()
	return &logger
}

func (cl *contextLogger) Application() *zerolog.Logger {
	l := cl.Logger.Application()
	with := l.With().
		Str("request_id", cl.rc.RequestID).
		Str("trace_id", cl.rc.TraceID).
		Str("span_id", cl.rc.SpanID).
		Str("tenant_id", cl.rc.TenantID).
		Str("user_id", cl.rc.UserID).
		Str("method", cl.rc.Method).
		Str("tool", cl.rc.Tool)
	logger := with.Logger()
	return &logger
}

func (cl *contextLogger) Security() *zerolog.Logger {
	l := cl.Logger.Security()
	with := l.With().
		Str("request_id", cl.rc.RequestID).
		Str("trace_id", cl.rc.TraceID).
		Str("span_id", cl.rc.SpanID).
		Str("tenant_id", cl.rc.TenantID).
		Str("user_id", cl.rc.UserID).
		Str("method", cl.rc.Method).
		Str("tool", cl.rc.Tool)
	logger := with.Logger()
	return &logger
}

func (cl *contextLogger) Network() *zerolog.Logger {
	l := cl.Logger.Network()
	with := l.With().
		Str("request_id", cl.rc.RequestID).
		Str("trace_id", cl.rc.TraceID).
		Str("span_id", cl.rc.SpanID).
		Str("tenant_id", cl.rc.TenantID).
		Str("user_id", cl.rc.UserID).
		Str("method", cl.rc.Method).
		Str("tool", cl.rc.Tool)
	logger := with.Logger()
	return &logger
}

func (cl *contextLogger) WithContext(ctx context.Context) Logger {
	newRC := RequestContextFrom(ctx)
	merged := *cl.rc
	if newRC.RequestID != "" {
		merged.RequestID = newRC.RequestID
	}
	if newRC.TraceID != "" {
		merged.TraceID = newRC.TraceID
	}
	if newRC.SpanID != "" {
		merged.SpanID = newRC.SpanID
	}
	if newRC.TenantID != "" {
		merged.TenantID = newRC.TenantID
	}
	if newRC.UserID != "" {
		merged.UserID = newRC.UserID
	}
	if newRC.Method != "" {
		merged.Method = newRC.Method
	}
	if newRC.Tool != "" {
		merged.Tool = newRC.Tool
	}
	return &contextLogger{Logger: cl.Logger, rc: &merged}
}

func (cl *contextLogger) WithFields(fields map[string]any) Logger {
	return &contextFieldsLogger{contextLogger: cl, fields: fields}
}

type contextFieldsLogger struct {
	*contextLogger
	fields map[string]any
}

func (cfl *contextFieldsLogger) System() *zerolog.Logger {
	l := cfl.contextLogger.System()
	with := l.With().Fields(cfl.fields)
	logger := with.Logger()
	return &logger
}

func (cfl *contextFieldsLogger) Application() *zerolog.Logger {
	l := cfl.contextLogger.Application()
	with := l.With().Fields(cfl.fields)
	logger := with.Logger()
	return &logger
}

func (cfl *contextFieldsLogger) Security() *zerolog.Logger {
	l := cfl.contextLogger.Security()
	with := l.With().Fields(cfl.fields)
	logger := with.Logger()
	return &logger
}

func (cfl *contextFieldsLogger) Network() *zerolog.Logger {
	l := cfl.contextLogger.Network()
	with := l.With().Fields(cfl.fields)
	logger := with.Logger()
	return &logger
}

func (cfl *contextFieldsLogger) WithFields(fields map[string]any) Logger {
	merged := make(map[string]any, len(cfl.fields)+len(fields))
	for k, v := range cfl.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &contextFieldsLogger{contextLogger: cfl.contextLogger, fields: merged}
}

// fieldsLogger wraps Logger to add static fields.
type fieldsLogger struct {
	Logger
	fields map[string]any
}

func (fl *fieldsLogger) System() *zerolog.Logger {
	l := fl.Logger.System()
	with := l.With().Fields(fl.fields)
	logger := with.Logger()
	return &logger
}

func (fl *fieldsLogger) Application() *zerolog.Logger {
	l := fl.Logger.Application()
	with := l.With().Fields(fl.fields)
	logger := with.Logger()
	return &logger
}

func (fl *fieldsLogger) Security() *zerolog.Logger {
	l := fl.Logger.Security()
	with := l.With().Fields(fl.fields)
	logger := with.Logger()
	return &logger
}

func (fl *fieldsLogger) Network() *zerolog.Logger {
	l := fl.Logger.Network()
	with := l.With().Fields(fl.fields)
	logger := with.Logger()
	return &logger
}

func (fl *fieldsLogger) WithFields(fields map[string]any) Logger {
	merged := make(map[string]any, len(fl.fields)+len(fields))
	for k, v := range fl.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &fieldsLogger{Logger: fl.Logger, fields: merged}
}

func (fl *fieldsLogger) WithContext(ctx context.Context) Logger {
	return fl.Logger.WithContext(ctx)
}

// parseLevel converts string to LogLevel.
func parseLevel(s string) LogLevel {
	switch s {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// toZerologLevel converts LogLevel to zerolog.Level.
func toZerologLevel(l LogLevel) zerolog.Level {
	switch l {
	case LevelDebug:
		return zerolog.DebugLevel
	case LevelInfo:
		return zerolog.InfoLevel
	case LevelWarn:
		return zerolog.WarnLevel
	case LevelError:
		return zerolog.ErrorLevel
	case LevelFatal:
		return zerolog.FatalLevel
	default:
		return zerolog.InfoLevel
	}
}

// rotatingWriter handles log rotation (simplified - integrates with rotation.go later).
type rotatingWriter struct {
	*os.File
}

func newRotatingWriter(dir, filename string, maxSizeMB, maxAgeDays int, compress bool) io.Writer {
	f, err := os.OpenFile(filepath.Join(dir, filename), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return os.Stderr
	}
	return &rotatingWriter{File: f}
}

// securityWriter writes security logs with immediate sync.
type securityWriter struct {
	*os.File
}

func newSecurityWriter(dir, filename string, maxAgeDays int) io.Writer {
	f, err := os.OpenFile(filepath.Join(dir, filename), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return os.Stderr
	}
	return &securityWriter{File: f}
}

func (sw *securityWriter) Write(p []byte) (n int, err error) {
	n, err = sw.File.Write(p)
	if err == nil {
		sw.File.Sync()
	}
	return n, err
}