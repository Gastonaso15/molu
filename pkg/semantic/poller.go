package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// HealthChecker interface to avoid circular package dependency with pkg/health.
type HealthChecker interface {
	GatedCheck() error
}

// SchemaPoller manages periodic refreshing of the semantic map from xolu.
type SchemaPoller struct {
	xoluURL      string
	authMode     string
	token        string
	tenant       string
	pollInterval time.Duration
	store        *SemanticStore
	health       HealthChecker
	client       *http.Client
}

// NewSchemaPoller creates a new SchemaPoller instance.
func NewSchemaPoller(
	xoluURL, authMode, token, tenant string,
	pollInterval time.Duration,
	store *SemanticStore,
	health HealthChecker,
) *SchemaPoller {
	return &SchemaPoller{
		xoluURL:      strings.TrimRight(xoluURL, "/"),
		authMode:     authMode,
		token:        token,
		tenant:       tenant,
		pollInterval: pollInterval,
		store:        store,
		health:       health,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// PollOnce fetches schemas from xolu and updates the store atomically.
func (p *SchemaPoller) PollOnce(ctx context.Context) error {
	if p.health != nil {
		if err := p.health.GatedCheck(); err != nil {
			// Substrate not ready; skip polling this cycle
			return err
		}
	}

	newMap := &SemanticMap{
		Entities:  make(map[string]*EntityDef),
		Machines:  make(map[string]*MachineDef),
		Sequences: make(map[string]*GenDef),
		Events:    make(map[string]*EventDef),
		Tenant:    p.tenant,
		ReadAt:    time.Now(),
	}

	// 1. Fetch Entity Schemas
	_ = p.fetchEntities(ctx, newMap)

	// 2. Fetch FSM Definitions
	_ = p.fetchFSMs(ctx, newMap)

	// 3. Fetch Generators and Sequences
	_ = p.fetchGenerators(ctx, newMap)

	// 4. Fetch Event Definitions
	_ = p.fetchEvents(ctx, newMap)

	// Atomically swap the new map
	p.store.Update(newMap)
	slog.Info("Semantic map updated successfully",
		"entities_count", len(newMap.Entities),
		"machines_count", len(newMap.Machines),
		"sequences_count", len(newMap.Sequences),
		"events_count", len(newMap.Events),
	)
	return nil
}

// Start launches the periodic background refresh loop. Callers are expected to
// have already run an initial PollOnce so the semantic map is populated before
// the MCP transport opens (spec Part 2 §8.4).
func (p *SchemaPoller) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := p.PollOnce(ctx); err != nil {
					slog.Warn("Schema refresh poll failed; retaining previous semantic map", "error", err)
				}
			}
		}
	}()
}

func (p *SchemaPoller) fetchEntities(ctx context.Context, target *SemanticMap) error {
	path := "/api/v1/_schema"
	data, err := p.doGet(ctx, path)
	if err != nil {
		return err
	}

	var rawEntities []map[string]interface{}
	if err := json.Unmarshal(data, &rawEntities); err == nil {
		for _, e := range rawEntities {
			name, _ := e["name"].(string)
			if name == "" {
				continue
			}
			schemaRaw, _ := json.Marshal(e["schema"])
			def := &EntityDef{
				Name:   name,
				Schema: schemaRaw,
			}
			target.Entities[name] = def
		}
		return nil
	}

	var entitiesMap map[string]interface{}
	if err := json.Unmarshal(data, &entitiesMap); err == nil {
		for name, schemaObj := range entitiesMap {
			schemaRaw, _ := json.Marshal(schemaObj)
			target.Entities[name] = &EntityDef{
				Name:   name,
				Schema: schemaRaw,
			}
		}
	}
	return nil
}

func (p *SchemaPoller) fetchFSMs(ctx context.Context, target *SemanticMap) error {
	path := "/api/v2/fsm/def"
	data, err := p.doGet(ctx, path)
	if err != nil {
		return err
	}

	var machines []MachineDef
	if err := json.Unmarshal(data, &machines); err == nil {
		for _, m := range machines {
			copyM := m
			target.Machines[m.ID] = &copyM
		}
	}
	return nil
}

func (p *SchemaPoller) fetchGenerators(ctx context.Context, target *SemanticMap) error {
	path := "/api/v2/gen/seq"
	data, err := p.doGet(ctx, path)
	if err != nil {
		return err
	}

	var seqs map[string]GenDef
	if err := json.Unmarshal(data, &seqs); err == nil {
		for name, g := range seqs {
			copyG := g
			target.Sequences[name] = &copyG
		}
	}
	return nil
}

func (p *SchemaPoller) fetchEvents(ctx context.Context, target *SemanticMap) error {
	path := "/api/v2/event/def"
	data, err := p.doGet(ctx, path)
	if err != nil {
		return err
	}

	var events []EventDef
	if err := json.Unmarshal(data, &events); err == nil {
		for _, ev := range events {
			copyEv := ev
			target.Events[ev.ID] = &copyEv
		}
	}
	return nil
}

func (p *SchemaPoller) doGet(ctx context.Context, path string) ([]byte, error) {
	reqURL := p.xoluURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	if p.tenant != "" {
		req.Header.Set("X-Tenant-ID", p.tenant)
	}

	if p.token != "" {
		if p.authMode == "apikey" {
			req.Header.Set("X-API-Key", p.token)
		} else {
			req.Header.Set("Authorization", "Bearer "+p.token)
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("endpoint %s returned status %d", path, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
