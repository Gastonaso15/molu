package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// defaultPollInterval is used when StartPolling gets a non-positive interval.
const defaultPollInterval = 60 * time.Second

// RemoteHubClient connects to a standalone Hub HTTP service, implementing
// CatalogLookup and ToolDiscoverer for Molu Frontend.
//
// It keeps two in-memory caches so that a transient Hub outage does not
// empty the surface offered to the agent (stale-while-error):
//   - the registry catalogue, refreshed by StartPolling and on demand;
//   - the last valid discovery answer per requester context.
//
// All methods are safe for concurrent use.
type RemoteHubClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration

	mu       sync.RWMutex
	logger   telemetry.Logger
	catalog  map[string]ToolRegistration
	hasCache bool
	lastList map[string][]rpi.ScoredTool
}

// NewRemoteHubClient builds a client configured to communicate with the Hub.
func NewRemoteHubClient(baseURL string, timeout time.Duration) *RemoteHubClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &RemoteHubClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout:  timeout,
		lastList: make(map[string][]rpi.ScoredTool),
	}
}

// SetLogger sets the logger used to report polling failures. Optional: with
// no logger, failures are retained silently.
func (c *RemoteHubClient) SetLogger(l telemetry.Logger) {
	c.mu.Lock()
	c.logger = l
	c.mu.Unlock()
}

func (c *RemoteHubClient) warn(msg string, err error) {
	c.mu.RLock()
	l := c.logger
	c.mu.RUnlock()
	if l != nil {
		l.Network().Warn().Err(err).Str("hub_url", c.baseURL).Msg(msg)
	}
}

// StartPolling refreshes the registry catalogue now and then every interval
// until ctx is cancelled. A failed poll is logged and the last valid catalogue
// is kept untouched. It returns immediately; polling runs in a goroutine.
func (c *RemoteHubClient) StartPolling(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	go func() {
		c.poll(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.poll(ctx)
			}
		}
	}()
}

func (c *RemoteHubClient) poll(ctx context.Context) {
	if err := c.refresh(ctx); err != nil && ctx.Err() == nil {
		c.warn("hub catalogue poll failed; keeping last valid catalogue", err)
	}
}

// refresh fetches the registry listing and, only on success, replaces the cache.
func (c *RemoteHubClient) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	reqBody, _ := json.Marshal(map[string]string{})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/registry/list", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub registry list: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Tools []ToolRegistration `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	next := make(map[string]ToolRegistration, len(result.Tools))
	for _, tool := range result.Tools {
		next[tool.Name] = tool
	}
	c.mu.Lock()
	c.catalog, c.hasCache = next, true
	c.mu.Unlock()
	return nil
}

// Lookup returns a tool by name. A cached tool is served without a network
// call; on a miss the catalogue is refreshed on demand once. If the Hub cannot
// be reached, the last valid catalogue answers and, if there never was one,
// the tool is not offered.
func (c *RemoteHubClient) Lookup(name string) (ToolRegistration, bool) {
	c.mu.RLock()
	tool, ok := c.catalog[name]
	c.mu.RUnlock()
	if ok {
		return tool, true
	}

	if err := c.refresh(context.Background()); err != nil {
		c.warn("hub catalogue refresh failed", err)
		return ToolRegistration{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	tool, ok = c.catalog[name]
	return tool, ok
}

// ListTools queries the remote Hub for tools admitted and scored for the requester.
// When the Hub is unavailable and a valid answer for the same requester context
// was obtained before, that answer is served; without one the typed
// SUBSTRATE_UNAVAILABLE error is returned. EMPTY and CONTRACT_VIOLATION are
// valid answers from the Hub and are never masked by retained data.
func (c *RemoteHubClient) ListTools(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error) {
	key := discoveryKey(actx)
	tools, err := c.listToolsRemote(ctx, actx)
	switch {
	case err == nil:
		c.mu.Lock()
		c.lastList[key] = tools
		c.mu.Unlock()
		return tools, nil
	case strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeSubstrateUnavailable)):
		c.mu.RLock()
		stale, ok := c.lastList[key]
		c.mu.RUnlock()
		if ok {
			c.warn("hub discovery unavailable; serving last valid catalogue", err)
			return stale, nil
		}
	case strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeEmpty)):
		c.mu.Lock()
		delete(c.lastList, key)
		c.mu.Unlock()
	}
	return nil, err
}

func discoveryKey(actx rpi.AdmitContext) string {
	b, _ := json.Marshal(actx)
	return string(b)
}

func (c *RemoteHubClient) listToolsRemote(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error) {
	reqBody, err := json.Marshal(actx)
	if err != nil {
		return nil, taxonomy.NewContractViolation(actx.Tenant, "tools/list", []string{"admit_context"})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/discovery/tools", bytes.NewReader(reqBody))
	if err != nil {
		return nil, taxonomy.NewSubstrateUnavailable(taxonomy.NowRFC3339(), taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 2*time.Second), 1, 3)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, taxonomy.NewSubstrateUnavailable(taxonomy.NowRFC3339(), taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 2*time.Second), 1, 3)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		var typedErr taxonomy.TypedError
		if err := json.NewDecoder(resp.Body).Decode(&typedErr); err == nil && typedErr.Code != "" {
			return nil, &typedErr
		}
		return nil, taxonomy.NewContractViolation(actx.Tenant, "tools/list", []string{"parameters"})
	}

	if resp.StatusCode != http.StatusOK {
		return nil, taxonomy.NewSubstrateUnavailable(taxonomy.NowRFC3339(), taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 2*time.Second), 1, 3)
	}

	var result struct {
		Tools []rpi.ScoredTool `json:"tools"`
		Code  string           `json:"code,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, taxonomy.NewSubstrateUnavailable(taxonomy.NowRFC3339(), taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 2*time.Second), 1, 3)
	}

	if result.Code == "EMPTY" || len(result.Tools) == 0 {
		return nil, taxonomy.NewEmpty(actx.Tenant, "tools/list", nil)
	}

	return result.Tools, nil
}
