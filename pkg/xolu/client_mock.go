package xolu

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Gastonaso15/molu/pkg/telemetry"
)

type MockXoluClient struct {
	mu           sync.Mutex
	WalkFunc     func(ctx context.Context, req WalkRequest) (WalkResponse, error)
	FindFunc     func(ctx context.Context, req FindRequest) (FindResponse, error)
	GetFunc      func(ctx context.Context, req GetRequest) (GetResponse, error)
	PingFunc     func(ctx context.Context) error
	ReadyFunc    func(ctx context.Context) error
	ExecuteFunc  func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error)
	WalkCalls    []WalkRequest
	FindCalls    []FindRequest
	GetCalls     []GetRequest
	PingCalls    int
	ReadyCalls   int
	logger       telemetry.Logger
	metrics      telemetry.Metrics
}

func (m *MockXoluClient) Execute(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
	m.mu.Lock()
	fn := m.ExecuteFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, primitive, args)
	}
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *MockXoluClient) Walk(ctx context.Context, req WalkRequest) (WalkResponse, error) {
	start := time.Now()
	
	m.mu.Lock()
	m.WalkCalls = append(m.WalkCalls, req)
	fn := m.WalkFunc
	m.mu.Unlock()
	
	var err error
	if fn != nil {
		_, err = fn(ctx, req)
	} else {
		err = errors.New("Walk not implemented")
	}
	
	// Log and metrics
	latencyMs := float64(time.Since(start).Milliseconds())
	code := "OK"
	if err != nil {
		// Try to extract error code
		var typedErr *TypedError
		if errors.As(err, &typedErr) {
			code = typedErr.Code
		} else {
			code = "UNKNOWN"
		}
	}
	
	if m.logger != nil {
		ctxLogger := m.logger.WithContext(ctx).WithFields(map[string]any{
			"tool":      req.Tool,
			"tenant_id": req.TenantID,
		})
		if err != nil {
			ctxLogger.Network().Error().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu walk failed")
		} else {
			ctxLogger.Network().Info().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu walk completed")
		}
	}
	
	if m.metrics != nil {
		m.metrics.ObserveLatency(telemetry.ComponentXoluClient, "walk", latencyMs)
		m.metrics.IncRequests(telemetry.ComponentXoluClient, "walk", req.Tool, code)
		if err != nil {
			m.metrics.IncErrors(telemetry.ComponentXoluClient, code)
		}
	}
	
	if fn != nil {
		return fn(ctx, req)
	}
	return WalkResponse{}, errors.New("Walk not implemented")
}

func (m *MockXoluClient) Find(ctx context.Context, req FindRequest) (FindResponse, error) {
	start := time.Now()
	
	m.mu.Lock()
	m.FindCalls = append(m.FindCalls, req)
	fn := m.FindFunc
	m.mu.Unlock()
	
	var err error
	if fn != nil {
		_, err = fn(ctx, req)
	} else {
		err = errors.New("Find not implemented")
	}
	
	latencyMs := float64(time.Since(start).Milliseconds())
	code := "OK"
	if err != nil {
		var typedErr *TypedError
		if errors.As(err, &typedErr) {
			code = typedErr.Code
		} else {
			code = "UNKNOWN"
		}
	}
	
	if m.logger != nil {
		ctxLogger := m.logger.WithContext(ctx).WithFields(map[string]any{
			"tenant_id": req.TenantID,
			"limit":     req.Limit,
			"offset":    req.Offset,
		})
		if err != nil {
			ctxLogger.Network().Error().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu find failed")
		} else {
			ctxLogger.Network().Info().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu find completed")
		}
	}
	
	if m.metrics != nil {
		m.metrics.ObserveLatency(telemetry.ComponentXoluClient, "find", latencyMs)
		m.metrics.IncRequests(telemetry.ComponentXoluClient, "find", "find", code)
		if err != nil {
			m.metrics.IncErrors(telemetry.ComponentXoluClient, code)
		}
	}
	
	if fn != nil {
		return fn(ctx, req)
	}
	return FindResponse{}, errors.New("Find not implemented")
}

func (m *MockXoluClient) Get(ctx context.Context, req GetRequest) (GetResponse, error) {
	start := time.Now()
	
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, req)
	fn := m.GetFunc
	m.mu.Unlock()
	
	var err error
	if fn != nil {
		_, err = fn(ctx, req)
	} else {
		err = errors.New("Get not implemented")
	}
	
	latencyMs := float64(time.Since(start).Milliseconds())
	code := "OK"
	if err != nil {
		var typedErr *TypedError
		if errors.As(err, &typedErr) {
			code = typedErr.Code
		} else {
			code = "UNKNOWN"
		}
	}
	
	if m.logger != nil {
		ctxLogger := m.logger.WithContext(ctx).WithFields(map[string]any{
			"id":        req.ID,
			"tenant_id": req.TenantID,
		})
		if err != nil {
			ctxLogger.Network().Error().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu get failed")
		} else {
			ctxLogger.Network().Info().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu get completed")
		}
	}
	
	if m.metrics != nil {
		m.metrics.ObserveLatency(telemetry.ComponentXoluClient, "get", latencyMs)
		m.metrics.IncRequests(telemetry.ComponentXoluClient, "get", "get", code)
		if err != nil {
			m.metrics.IncErrors(telemetry.ComponentXoluClient, code)
		}
	}
	
	if fn != nil {
		return fn(ctx, req)
	}
	return GetResponse{}, errors.New("Get not implemented")
}

func (m *MockXoluClient) Ping(ctx context.Context) error {
	start := time.Now()
	
	m.mu.Lock()
	m.PingCalls++
	fn := m.PingFunc
	m.mu.Unlock()
	
	var err error
	if fn != nil {
		err = fn(ctx)
	} else {
		err = errors.New("Ping not implemented")
	}
	
	latencyMs := float64(time.Since(start).Milliseconds())
	
	if m.logger != nil {
		ctxLogger := m.logger.WithContext(ctx)
		if err != nil {
			ctxLogger.Network().Error().
				Str("code", "PING_FAILED").
				Float64("latency_ms", latencyMs).
				Msg("xolu ping failed")
		} else {
			ctxLogger.Network().Info().
				Float64("latency_ms", latencyMs).
				Msg("xolu ping completed")
		}
	}
	
	if m.metrics != nil {
		m.metrics.ObserveLatency(telemetry.ComponentXoluClient, "ping", latencyMs)
	}
	
	if fn != nil {
		return fn(ctx)
	}
	return errors.New("Ping not implemented")
}

func (m *MockXoluClient) Ready(ctx context.Context) error {
	m.mu.Lock()
	m.ReadyCalls++
	fn := m.ReadyFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return errors.New("Ready not implemented")
}

func (m *MockXoluClient) SetLogger(logger telemetry.Logger) {
	m.logger = logger
}

func (m *MockXoluClient) SetMetrics(metrics telemetry.Metrics) {
	m.metrics = metrics
}

func (m *MockXoluClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WalkCalls = nil
	m.FindCalls = nil
	m.GetCalls = nil
	m.PingCalls = 0
	m.ReadyCalls = 0
}