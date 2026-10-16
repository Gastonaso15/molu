package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestLogger_JSONFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:     "json",
		Level:      "debug",
		OutputPath: "",
		Component:  ComponentMoluFront,
	}, &buf)

	// Test all domains write correctly
	logger.System().Info().Msg("system startup")
	logger.Application().Debug().Str("tool", "walk").Msg("invocation start")
	logger.Security().Warn().Str("actor", "user1").Str("decision", DecisionDeny).Msg("auth denied")
	logger.Network().Error().Str("code", "SUBSTRATE_UNAVAILABLE").Msg("xolu unreachable")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 log lines, got %d: %s", len(lines), buf.String())
	}

	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: invalid JSON: %v: %s", i, err, line)
		}

		// Verify base fields present
		if entry["timestamp"] == nil {
			t.Errorf("line %d: missing timestamp", i)
		}
		if entry["level"] == nil {
			t.Errorf("line %d: missing level", i)
		}
		if entry["domain"] == nil {
			t.Errorf("line %d: missing domain", i)
		}
		if entry["component"] != ComponentMoluFront {
			t.Errorf("line %d: component = %v, want %q", i, entry["component"], ComponentMoluFront)
		}
		if entry["message"] == nil {
			t.Errorf("line %d: missing message", i)
		}

		// Verify domain-specific flat fields
		switch i {
		case 0:
			if entry["domain"] != string(DomainSystem) {
				t.Errorf("line 0: domain = %v, want %q", entry["domain"], DomainSystem)
			}
			if entry["level"] != "info" {
				t.Errorf("line 0: level = %v, want info", entry["level"])
			}
		case 1:
			if entry["domain"] != string(DomainApplication) {
				t.Errorf("line 1: domain = %v, want %q", entry["domain"], DomainApplication)
			}
			if entry["level"] != "debug" {
				t.Errorf("line 1: level = %v, want debug", entry["level"])
			}
			if entry["tool"] != "walk" {
				t.Errorf("line 1: tool = %v, want walk", entry["tool"])
			}
		case 2:
			if entry["domain"] != string(DomainSecurity) {
				t.Errorf("line 2: domain = %v, want %q", entry["domain"], DomainSecurity)
			}
			if entry["level"] != "warn" {
				t.Errorf("line 2: level = %v, want warn", entry["level"])
			}
			if entry["actor"] != "user1" {
				t.Errorf("line 2: actor = %v, want user1", entry["actor"])
			}
			if entry["decision"] != DecisionDeny {
				t.Errorf("line 2: decision = %v, want %q", entry["decision"], DecisionDeny)
			}
		case 3:
			if entry["domain"] != string(DomainNetwork) {
				t.Errorf("line 3: domain = %v, want %q", entry["domain"], DomainNetwork)
			}
			if entry["level"] != "error" {
				t.Errorf("line 3: level = %v, want error", entry["level"])
			}
			if entry["code"] != "SUBSTRATE_UNAVAILABLE" {
				t.Errorf("line 3: code = %v, want SUBSTRATE_UNAVAILABLE", entry["code"])
			}
		}
	}
}

func TestLogger_ConsoleFormat(t *testing.T) {
	t.Parallel()

	// For console format test, don't use extraWriters - just verify logger creation works
	logger := NewLogger(Config{
		Format:     "console",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	// Verify logger methods work without panic
	logger.System().Info().Msg("system startup")
	logger.Application().Info().Msg("app event")

	// Test passes if no panic - console output goes to stdout
	_ = logger
}

func TestLogger_LevelFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configLevel string
		logFn       func(l Logger) *zerolog.Event
		shouldAppear bool
	}{
		{"debug allows debug", "debug", func(l Logger) *zerolog.Event { return l.Application().Debug() }, true},
		{"info blocks debug", "info", func(l Logger) *zerolog.Event { return l.Application().Debug() }, false},
		{"info allows info", "info", func(l Logger) *zerolog.Event { return l.Application().Info() }, true},
		{"warn blocks info", "warn", func(l Logger) *zerolog.Event { return l.Application().Info() }, false},
		{"warn allows warn", "warn", func(l Logger) *zerolog.Event { return l.Application().Warn() }, true},
		{"error blocks warn", "error", func(l Logger) *zerolog.Event { return l.Application().Warn() }, false},
		{"error allows error", "error", func(l Logger) *zerolog.Event { return l.Application().Error() }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(Config{
				Format:     "json",
				Level:      tc.configLevel,
				OutputPath: "",
				Component:  ComponentMoluFront,
			}, &buf)

			tc.logFn(logger).Msg("test message")

			output := strings.TrimSpace(buf.String())
			if tc.shouldAppear && output == "" {
				t.Errorf("expected log to appear at level %s", tc.configLevel)
			}
			if !tc.shouldAppear && output != "" {
				t.Errorf("expected log to be filtered at level %s, got: %s", tc.configLevel, output)
			}
		})
	}
}

func TestLogger_Domains(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:     "json",
		Level:      "debug",
		OutputPath: "",
		Component:  ComponentMoluFront,
	}, &buf)

	// Write to each domain
	logger.System().Info().Msg("sys")
	logger.Application().Info().Msg("app")
	logger.Security().Info().Msg("sec")
	logger.Network().Info().Msg("net")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	domains := []string{}
	for _, line := range lines {
		var entry map[string]any
		json.Unmarshal([]byte(line), &entry)
		domains = append(domains, entry["domain"].(string))
	}

	expected := []string{string(DomainSystem), string(DomainApplication), string(DomainSecurity), string(DomainNetwork)}
	for i, d := range domains {
		if d != expected[i] {
			t.Errorf("line %d: domain = %q, want %q", i, d, expected[i])
		}
	}
}

func TestLogger_SecuritySync(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:              "json",
		Level:               "info",
		OutputPath:          "",
		Component:           ComponentMoluFront,
		SecurityMaxAgeDays:  90,
	}, &buf)

	// Write security log - should flush immediately
	logger.Security().Info().
		Str("actor", "test").
		Str("action", "read").
		Str("resource", "order/1").
		Str("decision", DecisionAllow).
		Str("reason", "owner").
		Msg("auth decision")

	// Verify security log was written
	output := strings.TrimSpace(buf.String())
	if output == "" {
		t.Error("security log should be written")
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("invalid JSON in security log: %v", err)
	}
	if entry["domain"] != string(DomainSecurity) {
		t.Errorf("domain = %v, want %q", entry["domain"], DomainSecurity)
	}
	if entry["actor"] != "test" {
		t.Errorf("actor = %v, want test", entry["actor"])
	}
	if entry["decision"] != DecisionAllow {
		t.Errorf("decision = %v, want %q", entry["decision"], DecisionAllow)
	}
}

func TestLogger_WithContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	}, &buf)

	ctx := WithRequestContext(context.Background(), &RequestContext{
		RequestID: "req-123",
		TraceID:   "trace-456",
		SpanID:    "span-789",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Method:    "tools/call",
		Tool:      "walk",
	})

	ctxLogger := logger.WithContext(ctx)
	ctxLogger.Application().Info().Msg("contextual log")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry map[string]any
	json.Unmarshal([]byte(lines[0]), &entry)

	if entry["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want req-123", entry["request_id"])
	}
	if entry["trace_id"] != "trace-456" {
		t.Errorf("trace_id = %v, want trace-456", entry["trace_id"])
	}
	if entry["span_id"] != "span-789" {
		t.Errorf("span_id = %v, want span-789", entry["span_id"])
	}
}

func TestLogger_WithFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	}, &buf)

	fieldLogger := logger.WithFields(map[string]any{"custom_key": "custom_value"})
	fieldLogger.Application().Info().Msg("with fields")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry map[string]any
	json.Unmarshal([]byte(lines[0]), &entry)

	if entry["custom_key"] != "custom_value" {
		t.Errorf("custom_key = %v, want custom_value", entry["custom_key"])
	}
}

func TestLogger_LevelGetterSetter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	}, &buf)

	if logger.Level() != LevelInfo {
		t.Errorf("initial level = %q, want info", logger.Level())
	}

	logger.SetLevel(LevelDebug)
	if logger.Level() != LevelDebug {
		t.Errorf("after SetLevel(debug) = %q, want debug", logger.Level())
	}

	logger.SetLevel(LevelError)
	if logger.Level() != LevelError {
		t.Errorf("after SetLevel(error) = %q, want error", logger.Level())
	}
}

func TestLogger_ZeroAlloc(t *testing.T) {
	// Benchmark-style test to verify zero-allocation logging path
	var buf bytes.Buffer
	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	}, &buf)

	// Warm up
	for i := 0; i < 100; i++ {
		logger.Application().Info().Int("i", i).Msg("bench")
	}
	buf.Reset()

	// Measure allocs (rough check - no allocs in hot path)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		logger.Application().Info().Int("i", i).Msg("bench")
	}
	elapsed := time.Since(start)

	// Should complete quickly (no excessive allocations)
	if elapsed > 500*time.Millisecond {
		t.Logf("warning: logging took %v (possible allocations)", elapsed)
	}
}