package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SchemaClient: minimal interface to fetch schemas (mockable)
type SchemaClient interface {
	FetchSchemas(ctx context.Context) (Schemas, error)
}

// XoluSchemaClient: real implementation against xolu HTTP
type XoluSchemaClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

func NewXoluSchemaClient(baseURL string, timeout time.Duration) *XoluSchemaClient {
	return &XoluSchemaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

func (c *XoluSchemaClient) FetchSchemas(ctx context.Context) (Schemas, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/schemas", nil)
	if err != nil {
		return Schemas{}, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Schemas{}, fmt.Errorf("fetch schemas: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return Schemas{}, fmt.Errorf("xolu %d: %s", resp.StatusCode, resp.Status)
	}
	var schemas Schemas
	if err := json.NewDecoder(resp.Body).Decode(&schemas); err != nil {
		return Schemas{}, fmt.Errorf("decode schemas: %w", err)
	}
	return schemas, nil
}