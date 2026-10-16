package telemetry_integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/telemetry"
	"github.com/Gastonaso15/molu/pkg/xolu"
	"github.com/prometheus/client_golang/prometheus"
)

func TestIntegration_FullRequestFlow(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	// Setup logger with buffer to capture logs
	var logBuf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.Config{
		Format:     "json",
		Level:      "debug",
		OutputPath: "",
		Component:  telemetry.ComponentMoluFront,
	}, &logBuf)

	metrics := telemetry.NewMetricsWithRegistry(reg)

	// Create middleware
	middleware := telemetry.NewMiddleware(logger)

	// Create mock xolu client
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.WalkFunc = func(ctx context.Context, req xolu.WalkRequest) (xolu.WalkResponse, error) {
		return xolu.WalkResponse{Output: map[string]any{"status": "ok"}}, nil
	}
	mockXolu.SetLogger(logger)
	mockXolu.SetMetrics(metrics)

	// Create MCP handler that logs the request with context
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the incoming request with context (includes correlation IDs)
		ctx := r.Context()
		ctxLogger := logger.WithContext(ctx)
		ctxLogger.Application().Info().
			Str("method", "tools/call").
			Str("tool", "walk").
			Msg("mcp request received")

		// Simulate tool call
		_, err := mockXolu.Walk(ctx, xolu.WalkRequest{
			Tool:      "walk",
			Input:     map[string]any{"step": "start"},
			TenantID:  "tenant-1",
			Context:   map[string]any{},
		})
		if err != nil {
			ctxLogger.Network().Error().Err(err).Msg("xolu walk failed")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		ctxLogger.Application().Info().Msg("mcp request completed")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"result": "success"})
	})

	// Wrap with middleware
	wrappedHandler := middleware.Wrap(mcpHandler)

	// Make request
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"walk","arguments":{"step":"start"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	// Verify logs captured
	logOutput := logBuf.String()
	if logOutput == "" {
		t.Fatal("no logs captured")
	}

	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	t.Logf("Captured %d log lines", len(lines))

	// Verify at least application and network domains present (from our handler)
	domains := make(map[string]bool)
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if domain, ok := entry["domain"].(string); ok {
			domains[domain] = true
		}
	}

	// We log application and network domains
	if !domains[string(telemetry.DomainApplication)] {
		t.Errorf("missing application domain in logs")
	}
	if !domains[string(telemetry.DomainNetwork)] {
		t.Errorf("missing network domain in logs")
	}

	// Verify correlation IDs propagated in application logs
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["domain"] == string(telemetry.DomainApplication) {
			if entry["request_id"] == nil || entry["request_id"] == "" {
				t.Errorf("missing request_id in application log: %v", entry)
			}
			if entry["trace_id"] != "0af7651916cd43dd8448eb211c80319c" {
				t.Errorf("trace_id mismatch: %v", entry["trace_id"])
			}
		}
	}

	// Verify metrics
	metricsReq := httptest.NewRequest("GET", "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRec, metricsReq)
	metricsBody := metricsRec.Body.String()

	// Metrics are recorded by xolu-client component
	if !strings.Contains(metricsBody, `component="xolu-client"`) {
		t.Errorf("metrics missing xolu-client component: %s", metricsBody)
	}
	if !strings.Contains(metricsBody, `molu_requests_total{`) {
		t.Errorf("metrics missing request counter: %s", metricsBody)
	}
	if !strings.Contains(metricsBody, `molu_latency_seconds_bucket{`) {
		t.Errorf("metrics missing latency histogram: %s", metricsBody)
	}
}

func TestIntegration_TaxonomyErrorCorrelation(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	var logBuf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.Config{
		Format:     "json",
		Level:      "debug",
		OutputPath: "",
		Component:  telemetry.ComponentMoluFront,
	}, &logBuf)

	metrics := telemetry.NewMetricsWithRegistry(reg)

	mockXolu := &xolu.MockXoluClient{}
	mockXolu.WalkFunc = func(ctx context.Context, req xolu.WalkRequest) (xolu.WalkResponse, error) {
		return xolu.WalkResponse{}, &xolu.TypedError{
			Code:    "XOLU-MOLU-FRONT-NOT_FOUND",
			Message: "object not found",
			Details: map[string]any{"objectId": "obj-123"},
		}
	}
	mockXolu.SetLogger(logger)
	mockXolu.SetMetrics(metrics)

	middleware := telemetry.NewMiddleware(logger)

	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctxLogger := logger.WithContext(ctx)
		
		ctxLogger.Application().Info().
			Str("method", "tools/call").
			Str("tool", "walk").
			Msg("mcp request received")

		_, err := mockXolu.Walk(ctx, xolu.WalkRequest{
			Tool:      "walk",
			Input:     map[string]any{},
			TenantID:  "tenant-1",
		})
		if err != nil {
			ctxLogger.Network().Error().
				Str("code", "XOLU-MOLU-FRONT-NOT_FOUND").
				Err(err).
				Msg("xolu walk failed")
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware.Wrap(mcpHandler)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"walk"}}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	// Check logs for request_id in error details
	logOutput := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")

	foundRequestIDInError := false
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["domain"] == string(telemetry.DomainNetwork) && entry["level"] == "error" {
			if entry["request_id"] != nil && entry["request_id"] != "" {
				foundRequestIDInError = true
				t.Logf("Found request_id in network error log: %v", entry["request_id"])
			}
		}
	}

	if !foundRequestIDInError {
		t.Error("expected request_id in network error log")
	}

	// Check error metric
	metricsReq := httptest.NewRequest("GET", "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRec, metricsReq)
	metricsBody := metricsRec.Body.String()

	if !strings.Contains(metricsBody, `molu_errors_total{`) ||
		!strings.Contains(metricsBody, `component="xolu-client"`) ||
		!strings.Contains(metricsBody, `code="XOLU-MOLU-FRONT-NOT_FOUND"`) {
		t.Errorf("error metric not found: %s", metricsBody)
	}
}

func TestIntegration_SecurityAuthDecision(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.Config{
		Format:     "json",
		Level:      "debug",
		OutputPath: "",
		Component:  telemetry.ComponentMoluFront,
	}, &logBuf)

	// Test the security log when Admit returns empty
	logger.Security().Warn().
		Str("actor", "user-123").
		Str("action", "execute").
		Str("resource", "work_orders.CreateOrder").
		Str("decision", telemetry.DecisionDeny).
		Str("reason", "not_offered").
		Msg("rpi admit empty -> NOT_OFFERED")

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, `"domain":"security"`) {
		t.Error("security domain not found in logs")
	}
	if !strings.Contains(logOutput, `"decision":"DENY"`) {
		t.Error("DENY decision not found")
	}
	if !strings.Contains(logOutput, `"reason":"not_offered"`) {
		t.Error("reason not_offered not found")
	}
}

func TestIntegration_XoluUnavailable(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.Config{
		Format:     "json",
		Level:      "debug",
		OutputPath: "",
		Component:  telemetry.ComponentMoluFront,
	}, &logBuf)

	health := telemetry.NewHealth()

	// Set xolu as unavailable (stale)
	health.SetXoluStatus(false, time.Now().Add(-5*time.Minute))

	// Simulate network error log
	logger.Network().Error().
		Str("code", "XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE").
		Str("lastSuccessfulContact", time.Now().Add(-10*time.Minute).Format(time.RFC3339)).
		Str("nextRetryAt", time.Now().Add(30*time.Second).Format(time.RFC3339)).
		Int("attemptNumber", 3).
		Int("maxAttempts", 3).
		Msg("xolu unreachable")

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, `"domain":"network"`) {
		t.Error("network domain not found")
	}
	if !strings.Contains(logOutput, `"code":"XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE"`) {
		t.Error("SUBSTRATE_UNAVAILABLE code not found")
	}

	// Check health returns 503
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	health.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"status":"degraded"`) {
		t.Errorf("expected degraded status, got: %s", body)
	}
}

func TestIntegration_Shutdown(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  telemetry.ComponentMoluFront,
	}, &logBuf)

	// Simulate shutdown log
	logger.System().Info().
		Str("uptime", "1h30m").
		Int("requests_processed", 1500).
		Msg("shutdown initiated")

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, `"domain":"system"`) {
		t.Error("system domain not found")
	}
	if !strings.Contains(logOutput, `"message":"shutdown initiated"`) {
		t.Error("shutdown message not found")
	}
	if !strings.Contains(logOutput, `"uptime"`) {
		t.Error("uptime not in shutdown log")
	}
	if !strings.Contains(logOutput, `"requests_processed"`) {
		t.Error("requests_processed not in shutdown log")
	}
}

func TestIntegration_RotationUnderLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rotator := telemetry.NewLogRotator(telemetry.Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   10,
		MaxAgeDays:  7,
		Compress:    false,
		Filename:    "load-test.log",
	})

	// Override maxSize for test - 10KB to allow more data before rotation
	rotator.SetMaxSize(10240)

	// Concurrent writes
	done := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(id int) {
			data := strings.Repeat("x", 100) + "\n"
			for j := 0; j < 10; j++ {
				_, err := rotator.Write([]byte(data))
				if err != nil {
					done <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
			done <- nil
		}(i)
	}

	// Wait for all
	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Errorf("write error: %v", err)
		}
	}

	rotator.Close()

	// Verify files exist and no data loss
	files, err := filepath.Glob(filepath.Join(tmpDir, "load-test.log*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("no log files found after rotation")
	}

	// Read all content
	var totalLines int
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(content), "\n")
		totalLines += len(lines) - 1
	}

	if totalLines < 180 {
		t.Errorf("expected ~200 lines, got %d (possible data loss)", totalLines)
	}
}