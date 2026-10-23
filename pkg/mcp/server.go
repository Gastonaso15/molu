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
	"strings"
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
	} else {
		initialPrims = StandardPrimitives()
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

// callToolParams represents parameters for tools/call.
type callToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (s *Server) isToolOffered(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var prims []schema.PrimitiveSchema
	if s.provider != nil {
		prims = s.provider.GetPrimitives()
	} else {
		prims = s.primitives
	}
	for _, p := range prims {
		if p.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) handleCallTool(ctx context.Context, params json.RawMessage) (interface{}, *jsonRPCError) {
	var p callToolParams
	if trimmed := bytes.TrimSpace(params); len(trimmed) > 0 && string(trimmed) != "null" {
		if err := json.Unmarshal(trimmed, &p); err != nil {
			return nil, typedRPCError(taxonomy.NewContractViolation("", "tools/call", []string{"params"}))
		}
	} else {
		return nil, typedRPCError(taxonomy.NewContractViolation("", "tools/call", []string{"params"}))
	}

	// 1. Health check gating (RF-4)
	if s.health != nil {
		if err := s.health.Check(); err != nil {
			var te *taxonomy.TypedError
			if errors.As(err, &te) {
				return nil, typedRPCError(te)
			}
			return nil, typedRPCError(taxonomy.NewSubstrateUnavailable("", "", 0, 0))
		}
	}

	// 2. Check if tool is offered (RF-5)
	if !s.isToolOffered(p.Name) {
		return nil, typedRPCError(taxonomy.NewNotOffered("", p.Name))
	}

	// 3. Contract validation (RF-3 / T4)
	if s.provider != nil {
		if te := s.provider.ValidateInput(p.Name, p.Arguments); te != nil {
			return nil, typedRPCError(te)
		}
	} else {
		res := schema.ValidateInput(StandardPrimitives(), p.Name, p.Arguments)
		if !res.Valid {
			return nil, typedRPCError(taxonomy.NewContractViolation("", p.Name, res.InvalidFields))
		}
	}

	// 4. Execution and taxonomy mapping (T5)
	if p.Name == "describe" && s.executor == nil {
		scope, _ := p.Arguments["scope"].(string)
		if scope == "" {
			scope = "all"
		}
		descResult := map[string]interface{}{
			"scope":      scope,
			"primitives": StandardPrimitives(),
		}
		data, _ := json.Marshal(descResult)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(data),
				},
			},
		}, nil
	}

	if s.executor != nil {
		execRes, err := s.executor.Execute(ctx, p.Name, p.Arguments)
		if err != nil {
			var te *taxonomy.TypedError
			if errors.As(err, &te) {
				return nil, typedRPCError(te)
			}

			msgLower := strings.ToLower(err.Error())
			isNotFound := strings.Contains(msgLower, "404") || strings.Contains(msgLower, "not found")

			switch p.Name {
			case "get":
				if isNotFound {
					return nil, typedRPCError(taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["id"])))
				}
			case "machine_state":
				if isNotFound {
					return nil, typedRPCError(taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["machine_id"])))
				}
			case "cal_confirm":
				if isNotFound {
					return nil, typedRPCError(taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["booking_id"])))
				}
			case "walk":
				if strings.Contains(msgLower, "guard") || strings.Contains(msgLower, "invalid transition") || strings.Contains(msgLower, "rejected") {
					return nil, typedRPCError(&taxonomy.TypedError{
						Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
						Message: err.Error(),
					})
				}
			}

			return nil, &jsonRPCError{Code: -32603, Message: err.Error()}
		}

		// Check for empty results mapping to EMPTY taxonomy
		switch p.Name {
		case "list":
			if isEmptyCollection(execRes, "items", "results") {
				return nil, typedRPCError(taxonomy.NewEmpty("", "list", map[string]any{"entity_type": p.Arguments["entity_type"]}))
			}
		case "query":
			if isEmptyCollection(execRes, "rows", "results") {
				return nil, typedRPCError(taxonomy.NewEmpty("", "query", map[string]any{"query": p.Arguments["query"]}))
			}
		case "machine_history":
			if isEmptyCollection(execRes, "history", "transitions", "items") {
				return nil, typedRPCError(taxonomy.NewEmpty("", "machine_history", map[string]any{"machine_id": p.Arguments["machine_id"]}))
			}
		case "cal_openings":
			if isEmptyCollection(execRes, "openings", "windows", "items") {
				return nil, typedRPCError(taxonomy.NewEmpty("", "cal_openings", map[string]any{"duration": p.Arguments["duration"]}))
			}
		}

		resBytes, _ := json.Marshal(execRes)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(resBytes),
				},
			},
		}, nil
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": `{"status":"ok"}`,
			},
		},
	}, nil
}

func isEmptyCollection(res map[string]interface{}, candidateKeys ...string) bool {
	if len(res) == 0 {
		return true
	}
	for _, key := range candidateKeys {
		if val, ok := res[key]; ok {
			switch v := val.(type) {
			case []interface{}:
				if len(v) == 0 {
					return true
				}
			case []map[string]interface{}:
				if len(v) == 0 {
					return true
				}
			}
		}
	}
	if count, ok := res["count"].(int); ok && count == 0 {
		return true
	}
	if count, ok := res["count"].(float64); ok && count == 0 {
		return true
	}
	return false
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
	case "tools/call":
		result, rpcErr = s.handleCallTool(ctx, req.Params)
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
