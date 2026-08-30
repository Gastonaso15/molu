package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ha1tch/molu/pkg/catalogue"
)

var validNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// PublisherRecord tracks publisher functions and last heartbeat.
type PublisherRecord struct {
	PublisherID   string
	Namespace     string
	LastHeartbeat time.Time
	Functions     map[string]catalogue.FunctionContract // keyed by FullName
}

// HubServer is the reference implementation of the Molu Hub.
type HubServer struct {
	addr             string
	tenant           string
	heartbeatTimeout time.Duration
	expiryInterval   time.Duration

	mu         sync.RWMutex
	publishers map[string]*PublisherRecord // keyed by PublisherID
}

// NewHubServer creates a new Molu Hub instance.
func NewHubServer(addr, tenant string, heartbeatTimeout, expiryInterval time.Duration) *HubServer {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = 90 * time.Second
	}
	if expiryInterval <= 0 {
		expiryInterval = 15 * time.Second
	}

	return &HubServer{
		addr:             addr,
		tenant:           tenant,
		heartbeatTimeout: heartbeatTimeout,
		expiryInterval:   expiryInterval,
		publishers:       make(map[string]*PublisherRecord),
	}
}

// Routes configures the HTTP router for the hub.
func (h *HubServer) Routes() http.Handler {
	mux := http.NewServeMux()

	// Observability
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Publisher Protocol
	mux.HandleFunc("/publish", h.handlePublish)
	mux.HandleFunc("/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("/unpublish", h.handleUnpublish)
	mux.HandleFunc("/whoami", h.handleWhoami)

	// Consumer Protocol
	mux.HandleFunc("/catalogue", h.handleCatalogue)
	mux.HandleFunc("/catalogue/", h.handleCatalogueParam)

	return mux
}

// Start runs the HTTP server and background expiry reaper.
func (h *HubServer) Start(ctx context.Context) error {
	h.startExpiryReaper(ctx)

	server := &http.Server{
		Addr:    h.addr,
		Handler: h.Routes(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	slog.Info("Starting Molu Hub reference server", "addr", h.addr, "tenant", h.tenant)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (h *HubServer) startExpiryReaper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(h.expiryInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.reapExpiredPublishers()
			}
		}
	}()
}

func (h *HubServer) reapExpiredPublishers() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for id, pub := range h.publishers {
		if now.Sub(pub.LastHeartbeat) > h.heartbeatTimeout {
			slog.Warn("Expiring silent publisher and removing its functions",
				"publisher", id,
				"namespace", pub.Namespace,
				"functions_count", len(pub.Functions),
			)
			delete(h.publishers, id)
		}
	}
}

func (h *HubServer) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var contract catalogue.FunctionContract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validation
	if !validNameRegex.MatchString(contract.Namespace) {
		http.Error(w, "Invalid namespace format", http.StatusBadRequest)
		return
	}
	if !validNameRegex.MatchString(contract.Name) {
		http.Error(w, "Invalid function name format", http.StatusBadRequest)
		return
	}
	if contract.Description == "" {
		http.Error(w, "Description is required", http.StatusBadRequest)
		return
	}
	if len(contract.InputSchema) == 0 {
		http.Error(w, "Input schema is required", http.StatusBadRequest)
		return
	}
	if u, err := url.ParseRequestURI(contract.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "Endpoint must be a valid http or https absolute URL", http.StatusBadRequest)
		return
	}

	publisherID := r.Header.Get("X-Publisher-ID")
	if publisherID == "" {
		publisherID = contract.Namespace
	}

	now := time.Now()
	contract.RegisteredAt = &now

	h.mu.Lock()
	pub, exists := h.publishers[publisherID]
	if !exists {
		pub = &PublisherRecord{
			PublisherID:   publisherID,
			Namespace:     contract.Namespace,
			LastHeartbeat: now,
			Functions:     make(map[string]catalogue.FunctionContract),
		}
		h.publishers[publisherID] = pub
	}
	pub.LastHeartbeat = now
	pub.Functions[contract.FullName()] = contract
	h.mu.Unlock()

	slog.Info("Function published successfully",
		"publisher", publisherID,
		"function", contract.FullName(),
		"endpoint", contract.Endpoint,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(contract)
}

func (h *HubServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	publisherID := r.Header.Get("X-Publisher-ID")
	if publisherID == "" {
		publisherID = r.URL.Query().Get("publisher")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	pub, exists := h.publishers[publisherID]
	if !exists {
		// Auto-register publisher entry if missing
		pub = &PublisherRecord{
			PublisherID:   publisherID,
			Namespace:     publisherID,
			LastHeartbeat: time.Now(),
			Functions:     make(map[string]catalogue.FunctionContract),
		}
		h.publishers[publisherID] = pub
	} else {
		pub.LastHeartbeat = time.Now()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "OK",
		"functions_count": len(pub.Functions),
		"expires_in_sec":  int(h.heartbeatTimeout.Seconds()),
	})
}

func (h *HubServer) handleUnpublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	publisherID := r.Header.Get("X-Publisher-ID")
	if publisherID == "" {
		publisherID = r.URL.Query().Get("publisher")
	}

	var toRemove []string
	_ = json.NewDecoder(r.Body).Decode(&toRemove)

	h.mu.Lock()
	defer h.mu.Unlock()

	pub, exists := h.publishers[publisherID]
	if !exists {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"removed": []string{}})
		return
	}

	removed := make([]string, 0)
	if len(toRemove) == 0 {
		for f := range pub.Functions {
			removed = append(removed, f)
		}
		delete(h.publishers, publisherID)
	} else {
		for _, name := range toRemove {
			if _, ok := pub.Functions[name]; ok {
				delete(pub.Functions, name)
				removed = append(removed, name)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"removed": removed,
	})
}

func (h *HubServer) handleWhoami(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"role":       []string{"publisher", "consumer"},
		"tenant":     h.tenant,
		"namespaces": []string{"*"},
	})
}

func (h *HubServer) handleCatalogue(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("name")

	h.mu.RLock()
	defer h.mu.RUnlock()

	var functions []catalogue.FunctionContract
	for _, pub := range h.publishers {
		if namespace != "" && pub.Namespace != namespace {
			continue
		}
		for _, fn := range pub.Functions {
			if name != "" && fn.Name != name {
				continue
			}
			functions = append(functions, fn)
		}
	}

	resp := catalogue.CatalogueResponse{
		Functions:   functions,
		GeneratedAt: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *HubServer) handleCatalogueParam(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/catalogue/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) == 1 && parts[0] != "" {
		// /catalogue/{namespace}
		namespace := parts[0]
		h.mu.RLock()
		defer h.mu.RUnlock()

		var functions []catalogue.FunctionContract
		for _, pub := range h.publishers {
			if pub.Namespace == namespace {
				for _, fn := range pub.Functions {
					functions = append(functions, fn)
				}
			}
		}

		resp := catalogue.CatalogueResponse{
			Functions:   functions,
			GeneratedAt: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	if len(parts) >= 2 {
		// /catalogue/{namespace}/{name}
		namespace := parts[0]
		name := parts[1]
		fullName := fmt.Sprintf("%s.%s", namespace, name)

		h.mu.RLock()
		defer h.mu.RUnlock()

		for _, pub := range h.publishers {
			if fn, ok := pub.Functions[fullName]; ok {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(fn)
				return
			}
		}

		http.Error(w, fmt.Sprintf("Function %q not found", fullName), http.StatusNotFound)
		return
	}

	h.handleCatalogue(w, r)
}
