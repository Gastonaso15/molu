package catalogue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// GenericTools defines the 13 reserved generic tool names.
var GenericTools = map[string]bool{
	"describe":        true,
	"get":             true,
	"list":            true,
	"create":          true,
	"update":          true,
	"query":           true,
	"walk":            true,
	"machine_state":   true,
	"machine_history": true,
	"cal_check":       true,
	"cal_openings":    true,
	"cal_propose":     true,
	"cal_confirm":     true,
}

// ContractAuth describes the authentication requirements to invoke the function.
type ContractAuth struct {
	Mode     string `json:"mode"`               // "none", "bearer", "apikey"
	Header   string `json:"header,omitempty"`   // defaults to "Authorization"
	TokenRef string `json:"token_ref,omitempty"`
}

// FunctionContract represents a published domain function contract.
type FunctionContract struct {
	Namespace            string          `json:"namespace"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema,omitempty"`
	Endpoint             string          `json:"endpoint"`
	Auth                 ContractAuth    `json:"auth"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	Idempotent           bool            `json:"idempotent"`
	Cost                 string          `json:"cost,omitempty"`    // "low", "moderate", "high"
	Latency              string          `json:"latency,omitempty"` // "sub-second", "seconds", "minutes"
	RegisteredAt         *time.Time      `json:"registered_at,omitempty"`
}

// FullName returns the namespaced tool name (e.g. "billing.CreateInvoice").
func (f *FunctionContract) FullName() string {
	if f.Namespace == "" {
		return f.Name
	}
	return fmt.Sprintf("%s.%s", f.Namespace, f.Name)
}

// CatalogueResponse is the envelope returned by GET /catalogue.
type CatalogueResponse struct {
	Functions   []FunctionContract `json:"functions"`
	GeneratedAt time.Time          `json:"generated_at"`
}

// CatalogueStore manages the active set of discovered domain functions.
type CatalogueStore struct {
	hubURL       string
	authMode     string
	token        string
	pollInterval time.Duration
	client       *http.Client

	mu        sync.RWMutex
	functions map[string]FunctionContract // keyed by FullName()
}

// NewCatalogueStore initializes a CatalogueStore.
func NewCatalogueStore(hubURL, authMode, token string, pollInterval time.Duration) *CatalogueStore {
	return &CatalogueStore{
		hubURL:       hubURL,
		authMode:     authMode,
		token:        token,
		pollInterval: pollInterval,
		client:       &http.Client{Timeout: 10 * time.Second},
		functions:    make(map[string]FunctionContract),
	}
}

// List returns a snapshot slice of all active domain function contracts.
func (c *CatalogueStore) List() []FunctionContract {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]FunctionContract, 0, len(c.functions))
	for _, fn := range c.functions {
		result = append(result, fn)
	}
	return result
}

// Get finds a function contract by full name.
func (c *CatalogueStore) Get(fullName string) (FunctionContract, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fn, ok := c.functions[fullName]
	return fn, ok
}

// Refresh polls the hub's GET /catalogue endpoint and updates the registered functions atomically.
func (c *CatalogueStore) Refresh(ctx context.Context) error {
	if c.hubURL == "" {
		return nil
	}

	reqURL := fmt.Sprintf("%s/catalogue", strings.TrimRight(c.hubURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}

	if c.token != "" {
		if c.authMode == "apikey" {
			req.Header.Set("X-API-Key", c.token)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach hub at %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hub returned status %d: %s", resp.StatusCode, string(body))
	}

	var catResp CatalogueResponse
	if err := json.NewDecoder(resp.Body).Decode(&catResp); err != nil {
		return fmt.Errorf("failed to decode catalogue response: %w", err)
	}

	newMap := make(map[string]FunctionContract)
	for _, fn := range catResp.Functions {
		fullName := fn.FullName()

		// Collision check: generic primitives are unnamespaced and always win
		if GenericTools[fullName] || (fn.Namespace == "" && GenericTools[fn.Name]) {
			slog.Error("Hub function collides with reserved generic primitive; skipping registration",
				"function", fullName)
			continue
		}

		newMap[fullName] = fn
	}

	c.mu.Lock()
	c.functions = newMap
	c.mu.Unlock()

	slog.Info("Hub catalogue refreshed successfully", "functions_count", len(newMap))
	return nil
}

// Start runs the periodic catalogue refresh loop.
func (c *CatalogueStore) Start(ctx context.Context) {
	if c.hubURL == "" {
		return
	}

	// Initial poll
	_ = c.Refresh(ctx)

	go func() {
		ticker := time.NewTicker(c.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Refresh(ctx); err != nil {
					slog.Warn("Catalogue refresh poll failed; retaining current catalogue", "error", err)
				}
			}
		}
	}()
}

// Invoke executes a domain function at its registered endpoint.
func (c *CatalogueStore) Invoke(ctx context.Context, fn FunctionContract, input interface{}) (interface{}, error) {
	payloadBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize function input: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fn.Endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid function endpoint %s: %w", fn.Endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply auth if required
	if fn.Auth.Mode == "bearer" && fn.Auth.TokenRef != "" {
		header := fn.Auth.Header
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, "Bearer "+fn.Auth.TokenRef)
	} else if fn.Auth.Mode == "apikey" && fn.Auth.TokenRef != "" {
		header := fn.Auth.Header
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, fn.Auth.TokenRef)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke function endpoint: %w", err)
	}
	defer resp.Body.Close()

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("endpoint returned invalid JSON response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("function endpoint returned error status %d: %v", resp.StatusCode, result)
	}

	return result, nil
}
