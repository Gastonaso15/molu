package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// RemoteHubClient connects to a standalone Hub HTTP service, implementing
// CatalogLookup and ToolDiscoverer for Molu Frontend.
type RemoteHubClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
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
		timeout: timeout,
	}
}

// Lookup queries the remote Hub for a tool by name.
func (c *RemoteHubClient) Lookup(name string) (ToolRegistration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	reqBody, _ := json.Marshal(map[string]string{})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/registry/list", bytes.NewReader(reqBody))
	if err != nil {
		return ToolRegistration{}, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ToolRegistration{}, false
	}
	defer resp.Body.Close()

	var result struct {
		Tools []ToolRegistration `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolRegistration{}, false
	}

	for _, tool := range result.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return ToolRegistration{}, false
}

// ListTools queries the remote Hub for tools admitted and scored for the requester.
func (c *RemoteHubClient) ListTools(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error) {
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

