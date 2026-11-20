package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
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

// detailConcurrency bounds the parallel reads of per-entity schemas.
const detailConcurrency = 8

// FetchSchemas reads what xolu offers: the list of entity types with a
// registered schema (GET /api/v1/schemas) and the JSON Schema of each one
// (GET /api/v1/schema/{entity}). A type whose schema disappeared between the
// two calls (404) is skipped; any other failure fails the whole fetch, so the
// loader keeps its previous snapshot and retries.
func (c *XoluSchemaClient) FetchSchemas(ctx context.Context) (Schemas, error) {
	body, status, err := c.get(ctx, "/api/v1/schemas")
	if err != nil {
		return Schemas{}, err
	}
	if status >= 500 {
		return Schemas{}, fmt.Errorf("xolu %d: %s", status, http.StatusText(status))
	}

	var list struct {
		Schemas
		Listed []struct {
			Name string `json:"name"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return Schemas{}, fmt.Errorf("decode schemas: %w", err)
	}

	out := list.Schemas
	if len(list.Listed) == 0 {
		return out, nil
	}

	entities := make([]*EntitySchema, len(list.Listed))
	errs := make([]error, len(list.Listed))
	sem := make(chan struct{}, detailConcurrency)
	var wg sync.WaitGroup
	for i, l := range list.Listed {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			entities[i], errs[i] = c.fetchEntity(ctx, name)
		}(i, l.Name)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return Schemas{}, err
		}
	}
	for _, e := range entities {
		if e != nil {
			out.Entities = append(out.Entities, *e)
		}
	}
	sort.Slice(out.Entities, func(i, j int) bool { return out.Entities[i].Name < out.Entities[j].Name })
	return out, nil
}

// fetchEntity returns nil, nil when xolu has no schema for the entity (404).
func (c *XoluSchemaClient) fetchEntity(ctx context.Context, name string) (*EntitySchema, error) {
	body, status, err := c.get(ctx, "/api/v1/schema/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusNotFound:
		return nil, nil
	case status >= 400:
		return nil, fmt.Errorf("xolu %d reading the schema of %q", status, name)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode schema of %q: %w", name, err)
	}
	return &EntitySchema{Name: name, Schema: doc}, nil
}

func (c *XoluSchemaClient) get(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch schemas: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read schemas: %w", err)
	}
	return body, resp.StatusCode, nil
}
