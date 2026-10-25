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
	"github.com/Gastonaso15/molu/pkg/telemetry"
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
	Metrics     telemetry.Metrics
}

// Server is the MCP server over stdio or any io.ReadWriter.
type Server struct {
	provider    PrimitiveProvider
	health      HealthChecker
	executor    SubstrateExecutor
	discovery   ToolDiscoverer
	callTimeout time.Duration
	metrics     telemetry.Metrics

	mu         sync.RWMutex
	primitives []schema.PrimitiveSchema

	writeMu sync.Mutex
	out     io.Writer
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
		metrics:     cfg.Metrics,
		primitives:  initialPrims,
	}
}

// SetOutput sets the destination writer for notifications and server messages.
func (s *Server) SetOutput(out io.Writer) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.out = out
}

// UpdatePrimitives atomically replaces the active primitives cache and emits
// the notifications/tools/list_changed notification.
func (s *Server) UpdatePrimitives(prims []schema.PrimitiveSchema) {
	s.mu.Lock()
	if prims == nil {
		s.primitives = []schema.PrimitiveSchema{}
	} else {
		s.primitives = make([]schema.PrimitiveSchema, len(prims))
		copy(s.primitives, prims)
	}
	s.mu.Unlock()

	_ = s.NotifyToolsListChanged()
}

// NotifyToolsListChanged sends a notifications/tools/list_changed notification to the client.
func (s *Server) NotifyToolsListChanged() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.out == nil {
		return nil
	}

	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
	}
	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	_, err = s.out.Write(append(data, '\n'))
	return err
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

	prims := s.primitives
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

	for _, p := range s.primitives {
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

	// Past this point the request is well-formed, so a failure is the tool's
	// outcome and not a protocol error. MCP clients show an isError result to
	// the agent, which is the only way it can read the typed code.
	result, rpcErr := s.executeTool(ctx, p)
	if rpcErr != nil {
		return toolErrorResult(rpcErr), nil
	}
	return result, nil
}

// executeTool runs a validated tools/call request: health gating, offer check,
// contract validation and execution. Failures come back as JSON-RPC errors and
// handleCallTool turns them into isError results.
func (s *Server) executeTool(ctx context.Context, p callToolParams) (interface{}, *jsonRPCError) {
	// 1. Health check gating (RF-4)
	if s.health != nil {
		if err := s.health.Check(); err != nil {
			var te *taxonomy.TypedError
			if errors.As(err, &te) {
				s.incError(te.Code)
				return nil, typedRPCError(te)
			}
			te = taxonomy.NewSubstrateUnavailable("", "", 0, 0)
			s.incError(te.Code)
			return nil, typedRPCError(te)
		}
	}

	// 2. Check if tool is offered (RF-5)
	if !s.isToolOffered(p.Name) {
		te := taxonomy.NewNotOffered("", p.Name)
		s.incError(te.Code)
		return nil, typedRPCError(te)
	}

	// 3. Contract validation (RF-3 / T4)
	if s.provider != nil {
		if te := s.provider.ValidateInput(p.Name, p.Arguments); te != nil {
			s.incError(te.Code)
			return nil, typedRPCError(te)
		}
	} else {
		res := schema.ValidateInput(StandardPrimitives(), p.Name, p.Arguments)
		if !res.Valid {
			te := taxonomy.NewContractViolation("", p.Name, res.InvalidFields)
			s.incError(te.Code)
			return nil, typedRPCError(te)
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
				s.incError(te.Code)
				return nil, typedRPCError(te)
			}

			msgLower := strings.ToLower(err.Error())
			isNotFound := strings.Contains(msgLower, "404") || strings.Contains(msgLower, "not found")
			var statusErr interface{ HTTPStatus() int }
			if errors.As(err, &statusErr) && statusErr.HTTPStatus() == 404 {
				isNotFound = true
			}

			switch p.Name {
			case "get":
				if isNotFound {
					te = taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["id"]))
					s.incError(te.Code)
					return nil, typedRPCError(te)
				}
			case "machine_state":
				if isNotFound {
					te = taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["machine_id"]))
					s.incError(te.Code)
					return nil, typedRPCError(te)
				}
			case "cal_confirm":
				if isNotFound {
					te = taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["booking_id"]))
					s.incError(te.Code)
					return nil, typedRPCError(te)
				}
			case "machine_history":
				if isNotFound {
					te = taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["machine_id"]))
					s.incError(te.Code)
					return nil, typedRPCError(te)
				}
			case "walk":
				if isNotFound {
					te = taxonomy.NewNotFound("", fmt.Sprintf("%v", p.Arguments["machine_id"]))
					s.incError(te.Code)
					return nil, typedRPCError(te)
				}
				if strings.Contains(msgLower, "guard") || strings.Contains(msgLower, "invalid transition") || strings.Contains(msgLower, "rejected") {
					te = &taxonomy.TypedError{
						Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
						Message: err.Error(),
					}
					s.incError(te.Code)
					return nil, typedRPCError(te)
				}
			}

			return nil, &jsonRPCError{Code: -32603, Message: err.Error()}
		}

		// Check for empty results mapping to EMPTY taxonomy
		switch p.Name {
		case "list":
			if isEmptyCollection(execRes, "data", "items", "results") {
				te := taxonomy.NewEmpty("", "list", map[string]any{"entity_type": p.Arguments["entity_type"]})
				s.incError(te.Code)
				return nil, typedRPCError(te)
			}
		case "query":
			if isEmptyCollection(execRes, "rows", "results") {
				te := taxonomy.NewEmpty("", "query", map[string]any{"query": p.Arguments["query"]})
				s.incError(te.Code)
				return nil, typedRPCError(te)
			}
		case "machine_history":
			if isEmptyCollection(execRes, "history", "transitions", "items") {
				te := taxonomy.NewEmpty("", "machine_history", map[string]any{"machine_id": p.Arguments["machine_id"]})
				s.incError(te.Code)
				return nil, typedRPCError(te)
			}
		case "cal_openings":
			if isEmptyCollection(execRes, "openings", "windows", "items") {
				te := taxonomy.NewEmpty("", "cal_openings", map[string]any{"duration": p.Arguments["duration"]})
				s.incError(te.Code)
				return nil, typedRPCError(te)
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

// toolErrorResult renders a failed tool call as an MCP result with isError set.
// A typed error travels whole as JSON text so the agent reads its code and
// details; any other failure carries its plain message.
func toolErrorResult(e *jsonRPCError) map[string]interface{} {
	text := e.Message
	if te, ok := e.Data.(*taxonomy.TypedError); ok {
		if data, err := json.Marshal(te); err == nil {
			text = string(data)
		}
	}
	return map[string]interface{}{
		"isError": true,
		"content": []map[string]interface{}{{"type": "text", "text": text}},
	}
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
		s.writeMu.Lock()
		_, errW := out.Write(append(data, '\n'))
		s.writeMu.Unlock()
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
	s.writeMu.Lock()
	_, err = out.Write(append(data, '\n'))
	s.writeMu.Unlock()
	return err
}

// Serve reads newline-delimited JSON-RPC messages from in and writes responses to out until EOF or ctx cancel.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.SetOutput(out)
	defer s.SetOutput(nil)

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

// incError increments the error counter for the given taxonomy code.
// It strips the prefix (XOLU-MOLU-FRONT- or XOLU-) to get the base code.
func (s *Server) incError(code string) {
	if s.metrics != nil {
		baseCode := taxonomy.StripPrefix(code)
		s.metrics.IncErrors(telemetry.ComponentMoluFront, string(baseCode))
	}
}
