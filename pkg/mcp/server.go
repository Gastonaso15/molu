package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ha1tch/molu/pkg/catalogue"
	"github.com/ha1tch/molu/pkg/exec"
)

// Server is the MCP server implementation for Molu.
type Server struct {
	executor  *exec.Executor
	catalogue *catalogue.CatalogueStore

	mu sync.Mutex
}

// NewServer initializes a new MCP Server instance.
func NewServer(executor *exec.Executor, cat *catalogue.CatalogueStore) *Server {
	return &Server{
		executor:  executor,
		catalogue: cat,
	}
}

// GenericToolsDefinitions returns the 13 generic tool definitions as specified in Part 2.
func (s *Server) GenericToolsDefinitions() []Tool {
	return []Tool{
		{
			Name:        "describe",
			Description: "Return xolu's operational schema: entity types, FSM definitions, generators, event definitions.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"scope": map[string]interface{}{
						"type":    "string",
						"enum":    []string{"all", "entities", "machines", "generators", "events"},
						"default": "all",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Optional. Restrict to one entity type, machine definition, generator, or event definition by name.",
					},
				},
			},
		},
		{
			Name:        "get",
			Description: "Retrieve a specific entity by type and id.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"entity_type", "id"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"id":          map[string]interface{}{"type": []string{"string", "integer"}},
				},
			},
		},
		{
			Name:        "list",
			Description: "List entities of a given type, optionally filtered.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"entity_type"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"filter":      map[string]interface{}{"type": "object", "description": "Field-value equality filters."},
					"limit":       map[string]interface{}{"type": "integer", "default": 50, "minimum": 1, "maximum": 500},
					"offset":      map[string]interface{}{"type": "integer", "default": 0, "minimum": 0},
				},
			},
		},
		{
			Name:        "create",
			Description: "Create a new entity.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"entity_type", "data"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"data":        map[string]interface{}{"type": "object", "description": "Field values for the new entity."},
				},
			},
		},
		{
			Name:        "update",
			Description: "Update fields on an existing entity.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"entity_type", "id", "changes"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"id":          map[string]interface{}{"type": []string{"string", "integer"}},
					"changes":     map[string]interface{}{"type": "object"},
					"version":     map[string]interface{}{"type": "integer", "description": "Optimistic concurrency version."},
				},
			},
		},
		{
			Name:        "query",
			Description: "Execute a query against xolu. Use OQL for tabular results, Sulpher for graph patterns.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"language", "query"},
				"properties": map[string]interface{}{
					"language": map[string]interface{}{"type": "string", "enum": []string{"oql", "sulpher"}},
					"query":    map[string]interface{}{"type": "string"},
					"params":   map[string]interface{}{"type": "object", "description": "Optional bind parameters."},
				},
			},
		},
		{
			Name:        "walk",
			Description: "Advance a state machine by input. The machine's guards will evaluate against variables and payload; the transition either applies or is rejected with the guard's diagnostic.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"machine_id", "input"},
				"properties": map[string]interface{}{
					"machine_id": map[string]interface{}{"type": []string{"string", "integer"}},
					"input":      map[string]interface{}{"type": "string"},
					"payload":    map[string]interface{}{"type": "object", "description": "Fields available to guards as payload.<field>."},
				},
			},
		},
		{
			Name:        "machine_state",
			Description: "Read current state, variables, and terminal status of a machine.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"machine_id"},
				"properties": map[string]interface{}{
					"machine_id": map[string]interface{}{"type": []string{"string", "integer"}},
				},
			},
		},
		{
			Name:        "machine_history",
			Description: "Read the transition history of a machine.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"machine_id"},
				"properties": map[string]interface{}{
					"machine_id": map[string]interface{}{"type": []string{"string", "integer"}},
					"limit":      map[string]interface{}{"type": "integer", "default": 20, "minimum": 1, "maximum": 500},
				},
			},
		},
		{
			Name:        "cal_check",
			Description: "Check availability against one or more calendars for a proposed time span, without creating a booking.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"calendars", "start", "end"},
				"properties": map[string]interface{}{
					"calendars": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"start":     map[string]interface{}{"type": "string", "format": "date-time"},
					"end":       map[string]interface{}{"type": "string", "format": "date-time"},
				},
			},
		},
		{
			Name:        "cal_openings",
			Description: "Find available windows across one or more calendars, given a duration and search range.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"calendars", "duration", "from", "to"},
				"properties": map[string]interface{}{
					"calendars": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"duration":  map[string]interface{}{"type": "string", "description": "ISO 8601 duration (e.g. PT1H)."},
					"from":      map[string]interface{}{"type": "string", "format": "date-time"},
					"to":        map[string]interface{}{"type": "string", "format": "date-time"},
					"objective": map[string]interface{}{"type": "string", "enum": []string{"earliest", "first-fit", "emptiest", "longest-clear-margin"}, "default": "earliest"},
					"limit":     map[string]interface{}{"type": "integer", "default": 10, "minimum": 1, "maximum": 100},
				},
			},
		},
		{
			Name:        "cal_propose",
			Description: "Create a proposed booking on one or more calendars. Bookings are placed on the proposed plane until confirmed.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"calendars", "start", "end"},
				"properties": map[string]interface{}{
					"calendars": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"start":     map[string]interface{}{"type": "string", "format": "date-time"},
					"end":       map[string]interface{}{"type": "string", "format": "date-time"},
					"payload":   map[string]interface{}{"type": "object", "description": "Optional application-defined metadata for the booking."},
				},
			},
		},
		{
			Name:        "cal_confirm",
			Description: "Confirm a previously proposed booking, moving it from the proposed plane to the binding plane.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"booking_id"},
				"properties": map[string]interface{}{
					"booking_id": map[string]interface{}{"type": []string{"string", "integer"}},
				},
			},
		},
	}
}

// ListAllTools returns the union of generic primitives and hub-discovered domain functions.
func (s *Server) ListAllTools() []Tool {
	tools := s.GenericToolsDefinitions()

	if s.catalogue != nil {
		for _, fn := range s.catalogue.List() {
			var inputSchema map[string]interface{}
			if len(fn.InputSchema) > 0 {
				_ = json.Unmarshal(fn.InputSchema, &inputSchema)
			}
			if inputSchema == nil {
				inputSchema = map[string]interface{}{"type": "object"}
			}

			tools = append(tools, Tool{
				Name:        fn.FullName(),
				Description: fn.Description,
				InputSchema: inputSchema,
			})
		}
	}

	return tools
}

// Dispatch handles a single JSON-RPC request and returns the corresponding response.
func (s *Server) Dispatch(ctx context.Context, req JSONRPCRequest) *JSONRPCResponse {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: ServerCapabilities{
				Tools: map[string]interface{}{},
			},
			ServerInfo: ServerInfo{
				Name:    "molu",
				Version: "0.2.0",
			},
		}
		return resp

	case "notifications/initialized":
		// Acknowledgement notification
		return nil

	case "ping":
		resp.Result = map[string]interface{}{}
		return resp

	case "tools/list":
		resp.Result = ToolsListResult{
			Tools: s.ListAllTools(),
		}
		return resp

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params: " + err.Error(),
			}
			return resp
		}

		result, err := s.CallTool(ctx, params.Name, params.Arguments)
		if err != nil {
			errText, _ := json.Marshal(map[string]interface{}{
				"error": err.Error(),
			})
			resp.Result = ToolCallResult{
				Content: []ToolCallContent{
					{Type: "text", Text: string(errText)},
				},
				IsError: true,
			}
			return resp
		}

		resBytes, _ := json.Marshal(result)
		resp.Result = ToolCallResult{
			Content: []ToolCallContent{
				{Type: "text", Text: string(resBytes)},
			},
			IsError: false,
		}
		return resp

	default:
		resp.Error = &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("Method %q not found", req.Method),
		}
		return resp
	}
}

// CallTool dispatches the execution to the appropriate generic or domain handler.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if args == nil {
		args = make(map[string]interface{})
	}

	// 1. Check if it's a domain function in the hub catalogue
	if s.catalogue != nil {
		if fn, ok := s.catalogue.Get(name); ok {
			return s.catalogue.Invoke(ctx, fn, args)
		}
	}

	// 2. Generic primitive dispatch
	switch name {
	case "describe":
		scope, _ := args["scope"].(string)
		item, _ := args["name"].(string)
		return s.executor.Describe(ctx, scope, item)

	case "get":
		entityType, _ := args["entity_type"].(string)
		id := args["id"]
		return s.executor.Get(ctx, entityType, id)

	case "list":
		entityType, _ := args["entity_type"].(string)
		filter, _ := args["filter"].(map[string]interface{})
		limit := getIntArg(args, "limit", 50)
		offset := getIntArg(args, "offset", 0)
		return s.executor.List(ctx, entityType, filter, limit, offset)

	case "create":
		entityType, _ := args["entity_type"].(string)
		data, _ := args["data"].(map[string]interface{})
		return s.executor.Create(ctx, entityType, data)

	case "update":
		entityType, _ := args["entity_type"].(string)
		id := args["id"]
		changes, _ := args["changes"].(map[string]interface{})
		var version *int
		if v, ok := args["version"].(float64); ok {
			vi := int(v)
			version = &vi
		}
		return s.executor.Update(ctx, entityType, id, changes, version)

	case "query":
		lang, _ := args["language"].(string)
		query, _ := args["query"].(string)
		params, _ := args["params"].(map[string]interface{})
		return s.executor.Query(ctx, lang, query, params)

	case "walk":
		machineID := args["machine_id"]
		input, _ := args["input"].(string)
		payload, _ := args["payload"].(map[string]interface{})
		return s.executor.Walk(ctx, machineID, input, payload)

	case "machine_state":
		machineID := args["machine_id"]
		return s.executor.MachineState(ctx, machineID)

	case "machine_history":
		machineID := args["machine_id"]
		limit := getIntArg(args, "limit", 20)
		return s.executor.MachineHistory(ctx, machineID, limit)

	case "cal_check":
		calendars := getStringSlice(args["calendars"])
		start, _ := args["start"].(string)
		end, _ := args["end"].(string)
		return s.executor.CalCheck(ctx, calendars, start, end)

	case "cal_openings":
		calendars := getStringSlice(args["calendars"])
		duration, _ := args["duration"].(string)
		from, _ := args["from"].(string)
		to, _ := args["to"].(string)
		objective, _ := args["objective"].(string)
		limit := getIntArg(args, "limit", 10)
		return s.executor.CalOpenings(ctx, calendars, duration, from, to, objective, limit)

	case "cal_propose":
		calendars := getStringSlice(args["calendars"])
		start, _ := args["start"].(string)
		end, _ := args["end"].(string)
		payload, _ := args["payload"].(map[string]interface{})
		return s.executor.CalPropose(ctx, calendars, start, end, payload)

	case "cal_confirm":
		bookingID := args["booking_id"]
		return s.executor.CalConfirm(ctx, bookingID)

	default:
		return nil, fmt.Errorf("tool %q not found", name)
	}
}

// RunStdio executes the MCP server over standard input and output.
func (s *Server) RunStdio(ctx context.Context) error {
	slog.Info("Starting Molu MCP Server in stdio mode")
	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCError{
					Code:    -32700,
					Message: "Parse error: " + err.Error(),
				},
			}
			s.writeStdioResponse(resp)
			continue
		}

		resp := s.Dispatch(ctx, req)
		if resp != nil {
			s.writeStdioResponse(*resp)
		}
	}
}

func (s *Server) writeStdioResponse(resp JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err == nil {
		fmt.Fprintf(os.Stdout, "%s\n", string(data))
	}
}

// RunHTTP runs the MCP server as a Streamable HTTP server.
func (s *Server) RunHTTP(ctx context.Context, addr, authMode, bearerToken string) error {
	mux := http.NewServeMux()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check HTTP Auth if configured
		if authMode == "bearer" && bearerToken != "" {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + bearerToken
			if authHeader != expected {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCError{
					Code:    -32700,
					Message: "Parse error: " + err.Error(),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		resp := s.Dispatch(r.Context(), req)
		if resp != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
	})

	mux.Handle("/", handler)
	mux.Handle("/mcp", handler)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	slog.Info("Starting Molu MCP Server in Streamable HTTP mode", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func getIntArg(args map[string]interface{}, key string, defVal int) int {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case string:
			var i int
			if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
				return i
			}
		}
	}
	return defVal
}

func getStringSlice(v interface{}) []string {
	if slice, ok := v.([]interface{}); ok {
		res := make([]string, 0, len(slice))
		for _, item := range slice {
			if s, ok := item.(string); ok {
				res = append(res, s)
			}
		}
		return res
	}
	if slice, ok := v.([]string); ok {
		return slice
	}
	return nil
}
