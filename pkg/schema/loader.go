package schema

import (
	"context"
	"sync"
	"time"
)

// LoaderConfig: configuration for retries and refresh
type LoaderConfig struct {
	RefreshInterval time.Duration // MOLU_FRONT_SCHEMA_REFRESH_INTERVAL
	RetryFloor      time.Duration // MOLU_FRONT_SCHEMA_RETRY_FLOOR
	RetryCeiling    time.Duration // MOLU_FRONT_SCHEMA_RETRY_CEILING
	MaxAttempts     int           // MOLU_FRONT_SCHEMA_MAX_ATTEMPTS (0 = unlimited)
	Timeout         time.Duration // MOLU_FRONT_SCHEMA_TIMEOUT
}

// SchemaLoader: atomic load + hot-reload
type SchemaLoader struct {
	mu            sync.RWMutex
	client        SchemaClient
	config        LoaderConfig
	primitives    []PrimitiveSchema // atomic snapshot for MCP registration
	stateMachines []StateMachine
	payloadTypes  []PayloadType
	lastLoad      time.Time
	lastError     error
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

func NewSchemaLoader(client SchemaClient, config LoaderConfig) *SchemaLoader {
	return &SchemaLoader{
		client:  client,
		config:  config,
		stopCh:  make(chan struct{}),
	}
}

// Load: initial load with backoff (RF-1, RF-3)
func (l *SchemaLoader) Load(ctx context.Context) error {
	attempt := 0
	delay := l.config.RetryFloor
	for {
		schemas, err := l.client.FetchSchemas(ctx)
		if err == nil {
			l.swap(schemas)
			return nil
		}
		l.lastError = err
		attempt++
		if l.config.MaxAttempts > 0 && attempt >= l.config.MaxAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay = min(delay*2, l.config.RetryCeiling)
		}
	}
}

// Refresh: hot reload (SIGHUP or ticker) — atomic swap (RF-4)
func (l *SchemaLoader) Refresh(ctx context.Context) error {
	schemas, err := l.client.FetchSchemas(ctx)
	if err != nil {
		return err
	}
	l.swap(schemas)
	return nil
}

// swap: atomic update under write lock
func (l *SchemaLoader) swap(schemas Schemas) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.primitives = schemas.Primitives
	l.stateMachines = schemas.StateMachines
	l.payloadTypes = schemas.PayloadTypes
	l.lastLoad = time.Now()
	l.lastError = nil
}

// GetPrimitives: immutable snapshot for MCP registration (concurrent read safe)
func (l *SchemaLoader) GetPrimitives() []PrimitiveSchema {
	l.mu.RLock()
	defer l.mu.RUnlock()
	// copy to avoid aliasing
	out := make([]PrimitiveSchema, len(l.primitives))
	copy(out, l.primitives)
	return out
}

// Run: refresh goroutine + signal handler (RF-4)
func (l *SchemaLoader) Run(ctx context.Context) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		ticker := time.NewTicker(l.config.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.stopCh:
				return
			case <-ticker.C:
				_ = l.Refresh(ctx) // error logged internally
			}
		}
	}()
}

// Stop: stops refresh goroutine
func (l *SchemaLoader) Stop() {
	close(l.stopCh)
	l.wg.Wait()
}