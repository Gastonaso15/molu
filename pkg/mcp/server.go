package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// PrimitiveProvider provides active primitive schemas (e.g. *schema.SchemaLoader).
type PrimitiveProvider interface {
	GetPrimitives() []schema.PrimitiveSchema
	ValidateInput(primitiveName string, input map[string]interface{}) *taxonomy.TypedError
}

// HealthChecker provides substrate availability status (e.g. *exec.Probe).
type HealthChecker interface {
	Check() error
}

// SubstrateExecutor executes primitive operations against substrate (e.g. xolu client).
type SubstrateExecutor interface {
	Execute(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error)
}

// ToolDiscoverer resolves the catalog tools offered to a requester (e.g.
// *hub.Discovery). When set, tools/list serves its result instead of the
// primitives.
type ToolDiscoverer interface {
	ListTools(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error)
}

// ServerConfig configures the MCP server.
type ServerConfig struct {
	Provider    PrimitiveProvider
	Health      HealthChecker
	Executor    SubstrateExecutor
	Discovery   ToolDiscoverer
	CallTimeout time.Duration
}

// Server is the MCP server over stdio or any io.ReadWriter.
type Server struct {
	provider    PrimitiveProvider
	health      HealthChecker
	executor    SubstrateExecutor
	discovery   ToolDiscoverer
	callTimeout time.Duration

	mu         sync.RWMutex
	primitives []schema.PrimitiveSchema
}

// NewServer creates a new MCP Server instance.
func NewServer(cfg ServerConfig) *Server {
	var initialPrims []schema.PrimitiveSchema
	if cfg.Provider != nil {
		initialPrims = cfg.Provider.GetPrimitives()
	}
	if initialPrims == nil {
		initialPrims = []schema.PrimitiveSchema{}
	}

	callTimeout := cfg.CallTimeout
	if callTimeout <= 0 {
		callTimeout = 30 * time.Second
	}

	return &Server{
		provider:    cfg.Provider,
		health:      cfg.Health,
		executor:    cfg.Executor,
		discovery:   cfg.Discovery,
		callTimeout: callTimeout,
		primitives:  initialPrims,
	}
}

// UpdatePrimitives atomically replaces the active primitives cache.
func (s *Server) UpdatePrimitives(prims []schema.PrimitiveSchema) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prims == nil {
		s.primitives = []schema.PrimitiveSchema{}
	} else {
		s.primitives = make([]schema.PrimitiveSchema, len(prims))
		copy(s.primitives, prims)
	}
}

// JSON-RPC 2.0 structures
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ToolDescriptor represents a tool exposed via tools/list according to MCP spec.
type ToolDescriptor struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// HandleListTools returns the list of active primitives formatted for tools/list.
func (s *Server) HandleListTools(ctx context.Context) []ToolDescriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var prims []schema.PrimitiveSchema
	if s.provider != nil {
		prims = s.provider.GetPrimitives()
	} else {
		prims = s.primitives
	}

	tools := make([]ToolDescriptor, 0, len(prims))
	for _, p := range prims {
		schemaMap := p.InputSchema
		if schemaMap == nil {
			schemaMap = map[string]interface{}{
				"type": "object",
			}
		}
		tools = append(tools, ToolDescriptor{
			Name:        p.Name,
			Description: p.Description,
			InputSchema: schemaMap,
		})
	}
	return tools
}

// listParams are the tools/list parameters: the requester context for the RPI
// plus the standard MCP pagination cursor, which is accepted and ignored.
type listParams struct {
	Tenant string   `json:"tenant"`
	Roles  []string `json:"roles"`
	Query  string   `json:"query"`
	Cursor string   `json:"cursor"`
}

// handleDiscoveryList serves tools/list from the Hub catalog filtered by the
// RPI. Typed errors keep their code and details in the JSON-RPC error data.
func (s *Server) handleDiscoveryList(ctx context.Context, params json.RawMessage) (interface{}, *jsonRPCError) {
	var p listParams
	if trimmed := bytes.TrimSpace(params); len(trimmed) > 0 && string(trimmed) != "null" {
		if err := json.Unmarshal(trimmed, &p); err != nil {
			return nil, typedRPCError(taxonomy.NewContractViolation("", "tools/list", []string{"params"}))
		}
	}

	scored, err := s.discovery.ListTools(ctx, rpi.AdmitContext{Tenant: p.Tenant, Roles: p.Roles, Query: p.Query})
	if err != nil {
		var te *taxonomy.TypedError
		if errors.As(err, &te) {
			return nil, typedRPCError(te)
		}
		return nil, &jsonRPCError{Code: -32603, Message: err.Error()}
	}

	tools := make([]ToolDescriptor, 0, len(scored))
	for _, t := range scored {
		schemaMap := t.InputSchema
		if schemaMap == nil {
			schemaMap = map[string]interface{}{"type": "object"}
		}
		tools = append(tools, ToolDescriptor{Name: t.Name, Description: t.Description, InputSchema: schemaMap})
	}
	return map[string]interface{}{"tools": tools}, nil
}

// typedRPCError maps a typed error onto a JSON-RPC error: CONTRACT_VIOLATION is
// invalid params, every other code a server error. The typed error travels
// whole in data so clients never lose the code or its details.
func typedRPCError(te *taxonomy.TypedError) *jsonRPCError {
	code := -32000
	if te.Code == taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) {
		code = -32602
	}
	return &jsonRPCError{Code: code, Message: te.Message, Data: te}
}

// HandleMessage parses a single JSON-RPC message and writes the response (if applicable).
func (s *Server) HandleMessage(ctx context.Context, msg []byte, out io.Writer) error {
	var req jsonRPCRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &jsonRPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}
		data, _ := json.Marshal(resp)
		_, errW := out.Write(append(data, '\n'))
		return errW
	}

	// Notifications don't have an ID and do not return a response
	isNotification := req.ID == nil

	var result interface{}
	var rpcErr *jsonRPCError

	switch req.Method {
	case "initialize":
		result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": true,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "molu",
				"version": "0.1.0",
			},
		}
	case "notifications/initialized":
		return nil
	case "ping":
		result = map[string]interface{}{}
	case "tools/list":
		if s.discovery != nil {
			result, rpcErr = s.handleDiscoveryList(ctx, req.Params)
			break
		}
		tools := s.HandleListTools(ctx)
		result = map[string]interface{}{
			"tools": tools,
		}
	default:
		rpcErr = &jsonRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	if isNotification {
		return nil
	}

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
		Error:   rpcErr,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = out.Write(append(data, '\n'))
	return err
}

// Serve reads newline-delimited JSON-RPC messages from in and writes responses to out until EOF or ctx cancel.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Support large schemas up to 4MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if err := s.HandleMessage(ctx, line, out); err != nil {
			return err
		}
	}

	return scanner.Err()
}

// ServeStdio serves the MCP protocol on os.Stdin and os.Stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.Serve(ctx, os.Stdin, os.Stdout)
}
