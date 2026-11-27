package bridge_integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/bridge"
	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/publisher/issues"
)

type fakeXoluIssues struct {
	mu       sync.Mutex
	def      map[string]any
	machines map[int64]*fakeMachineIssues
	entities map[string]map[int64]map[string]any
	machSeq  int64
	entSeq   int64
	defPosts        int
	lastWalkPayload map[string]any
}

type fakeMachineIssues struct {
	id    int64
	state string
}

func newFakeXoluIssues() *fakeXoluIssues {
	fx := &fakeXoluIssues{
		machines: map[int64]*fakeMachineIssues{},
		entities: map[string]map[int64]map[string]any{
			"assets": {
				200: {"id": int64(200), "name": "Main Compressor", "criticality": "Alta"},
				201: {"id": int64(201), "name": "Secondary Pump", "criticality": "Media"},
			},
			"issues": {},
		},
	}
	return fx
}

func (x *fakeXoluIssues) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v2/fsm/def", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		if r.Method == http.MethodGet {
			items := []any{}
			if x.def != nil {
				items = append(items, map[string]any{"id": int64(42), "name": x.def["name"]})
			}
			xoluJSON(w, 200, map[string]any{"definitions": items})
			return
		}
		var def map[string]any
		_ = json.NewDecoder(r.Body).Decode(&def)
		x.def = def
		x.defPosts++
		xoluJSON(w, 200, map[string]any{"id": int64(42), "name": def["name"]})
	})

	mux.HandleFunc("/api/v2/fsm/machine", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		x.machSeq++
		id := 1000 + x.machSeq
		m := &fakeMachineIssues{id: id, state: "reported"}
		x.machines[id] = m
		xoluJSON(w, 200, map[string]any{"id": id, "state": m.state})
	})

	mux.HandleFunc("/api/v2/fsm/machine/", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/api/v2/fsm/machine/")
		parts := strings.Split(path, "/")
		machID, _ := strconv.ParseInt(parts[0], 10, 64)
		m, ok := x.machines[machID]
		if !ok {
			xoluError(w, 404, "XOLU-FSM002", "machine not found")
			return
		}
		if len(parts) == 1 {
			xoluJSON(w, 200, map[string]any{"id": m.id, "state": m.state})
			return
		}
		if parts[1] == "walk" {
			var req struct {
				Input   string         `json:"input"`
				Payload map[string]any `json:"payload"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			next, ok := stepIssueLifecycle(m.state, req.Input)
			if !ok {
				xoluError(w, 400, "XOLU-FSM004", fmt.Sprintf("cannot transition from %s via %s", m.state, req.Input))
				return
			}
			m.state = next
			x.lastWalkPayload = req.Payload
			xoluJSON(w, 200, map[string]any{"current": m.state})
		}
	})

	mux.HandleFunc("/api/v1/assets/", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/assets/")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		asset, ok := x.entities["assets"][id]
		if !ok {
			xoluError(w, 404, "XOLU-ST001", "asset not found")
			return
		}
		xoluJSON(w, 200, asset)
	})

	mux.HandleFunc("/api/v1/issues", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		var doc map[string]any
		_ = json.NewDecoder(r.Body).Decode(&doc)
		x.entSeq++
		doc["id"] = x.entSeq
		x.entities["issues"][x.entSeq] = doc
		xoluJSON(w, 201, map[string]any{"id": x.entSeq, "data": doc})
	})

	mux.HandleFunc("/api/v1/issues/", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/issues/")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		doc, ok := x.entities["issues"][id]
		if !ok {
			xoluError(w, 404, "XOLU-ST001", "issue not found")
			return
		}
		if r.Method == http.MethodPatch {
			var patch map[string]any
			_ = json.NewDecoder(r.Body).Decode(&patch)
			for k, v := range patch {
				doc[k] = v
			}
			xoluJSON(w, 200, doc)
			return
		}
		xoluJSON(w, 200, doc)
	})

	return mux
}

type issuesE2EHarness struct {
	xolu      *fakeXoluIssues
	registry  *hub.Registry
	registrar *issues.Registrar
	stdin     *io.PipeWriter
	lines     chan string
	nextID    int
}

func setupIssuesE2E(t *testing.T) *issuesE2EHarness {
	t.Helper()
	const token = "issues-bearer-secret-token"

	// 1. Sustrato xolu
	fx := newFakeXoluIssues()
	xoluSrv := httptest.NewServer(fx.handler())
	t.Cleanup(xoluSrv.Close)

	// 2. Hub Standalone
	reg := hub.NewRegistry()
	provider, err := hub.NewRPIProvider("admit-all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hubSrv := httptest.NewServer(hub.NewHubHTTPServer(reg, hub.NewDiscovery(reg, provider)).Handler())
	t.Cleanup(hubSrv.Close)

	// 3. Issue Tracker Publisher auto-registrado en el Hub
	sub := issues.NewHTTPSubstrate(xoluSrv.URL, "", 2*time.Second)
	svc := issues.NewIssueService(sub)
	pubSrv := httptest.NewServer(issues.NewServer(svc, token).Handler())
	t.Cleanup(pubSrv.Close)

	registrar := issues.NewRegistrar(hubSrv.URL, pubSrv.URL, issues.DefaultBackoff())
	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("publisher registration: %v", err)
	}

	// 4. Molu Frontend (Execution Bridge + Remote Hub Client)
	t.Setenv("MOLU_FRONT_TOKEN_ISSUES_API_TOKEN", token)
	remoteHub := hub.NewRemoteHubClient(hubSrv.URL, 2*time.Second)
	execBridge := bridge.NewExecutionBridge(remoteHub, nil, nil, bridge.NewHTTPDomainCaller(nil, nil, 3*time.Second))

	srv := mcp.NewServer(mcp.ServerConfig{
		Discovery:      remoteHub,
		DomainExecutor: execBridge,
		CatalogLookup:  remoteHub,
		CallTimeout:    5 * time.Second,
		DefaultTenant:  "dev",
	})

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx, stdinR, stdoutW) }()

	lines := make(chan string, 50)
	go func() {
		rd := bufio.NewReader(stdoutR)
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			lines <- line
		}
	}()
	t.Cleanup(func() { cancel(); _ = stdinW.Close(); _ = stdoutW.Close() })

	return &issuesE2EHarness{xolu: fx, registry: reg, registrar: registrar, stdin: stdinW, lines: lines}
}

func (h *issuesE2EHarness) rpc(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	h.nextID++
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": h.nextID, "method": method, "params": params})
	if _, err := io.WriteString(h.stdin, string(req)+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-h.lines:
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		return resp
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", method)
		return nil
	}
}

func (h *issuesE2EHarness) call(t *testing.T, tool string, args map[string]any) map[string]any {
	return h.rpc(t, "tools/call", map[string]any{"name": tool, "arguments": args})
}

func TestIssues_E2E(t *testing.T) {
	h := setupIssuesE2E(t)

	// 1. Descubrimiento vía MCP tools/list
	list := h.rpc(t, "tools/list", map[string]any{})
	res, _ := list["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	seen := map[string]bool{}
	for _, tl := range tools {
		if m, ok := tl.(map[string]any); ok {
			seen[m["name"].(string)] = true
		}
	}
	for _, want := range []string{"issues.CreateIssue", "issues.GetIssue", "issues.AdvanceIssue"} {
		if !seen[want] {
			t.Fatalf("tools/list missing %s; got %v", want, seen)
		}
	}

	// 2. Creación de una incidencia con activo crítico y prioridad crítica
	createArgs := map[string]any{
		"tenant":      "tenant-alpha",
		"title":       "Turbine high vibration",
		"description": "Exceeded threshold on compressor 200",
		"priority":    "critical",
		"severity":    "critical",
		"asset_id":    200,
	}
	created := e2eSuccess(t, h.call(t, "issues.CreateIssue", createArgs))

	idFloat, ok := created["id"].(float64)
	if !ok || idFloat <= 0 {
		t.Fatalf("expected positive id, got %v", created["id"])
	}
	issueID := int64(idFloat)

	if created["title"] != "Turbine high vibration" || created["state"] != "reported" {
		t.Fatalf("unexpected created issue: %v", created)
	}
	if created["asset_category"] != "critico" {
		t.Errorf("expected asset_category critical, got %v", created["asset_category"])
	}
	if created["sla_first_response_deadline"] == nil || created["sla_resolution_deadline"] == nil {
		t.Errorf("expected SLA deadlines calculated, got %v", created)
	}

	// 3. Lectura de la incidencia creada vía issues.GetIssue
	got := e2eSuccess(t, h.call(t, "issues.GetIssue", map[string]any{"tenant": "tenant-alpha", "issue_id": issueID}))
	if got["title"] != "Turbine high vibration" || got["state"] != "reported" {
		t.Fatalf("retrieved issue mismatch: %v", got)
	}

	// 4. Rechazo de transición FSM inválida visible verbatim (Criterio 1)
	// Intentar cerrar directamente desde reported
	badAdvance := h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant":     "tenant-alpha",
		"issue_id":   issueID,
		"transition": "close",
	})
	resBad, _ := badAdvance["result"].(map[string]any)
	if isErr, _ := resBad["isError"].(bool); !isErr {
		t.Fatalf("expected isError: true on invalid transition, got %v", badAdvance)
	}
	errContent, ok := resBad["content"].([]any)
	if !ok || len(errContent) == 0 {
		t.Fatalf("missing error content: %v", resBad)
	}
	errMap := errContent[0].(map[string]any)
	errText, _ := errMap["text"].(string)

	if !strings.Contains(errText, "INVALID_TRANSITION") {
		t.Errorf("expected INVALID_TRANSITION in error text, got %q", errText)
	}
	if !strings.Contains(errText, "cannot transition from reported via close") {
		t.Errorf("expected verbatim diagnostic in error text, got %q", errText)
	}

	// 5. Transición válida hacia start (in_progress)
	goodAdvance := e2eSuccess(t, h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant":     "tenant-alpha",
		"issue_id":   issueID,
		"transition": "start",
		"actor_id":   101,
	}))
	if goodAdvance["new_state"] != "in_progress" || goodAdvance["previous_state"] != "reported" {
		t.Fatalf("expected reported -> in_progress, got %v", goodAdvance)
	}
	issMap, _ := goodAdvance["issue"].(map[string]any)
	if issMap["first_response_at"] == nil || issMap["first_response_at"] == "" {
		t.Errorf("expected first_response_at recorded, got %v", issMap)
	}
}


func stepIssueLifecycle(state, input string) (string, bool) {
	switch state {
	case "reported":
		switch input {
		case "triage":
			return "triaged", true
		case "start":
			return "in_progress", true
		case "reject":
			return "rejected", true
		case "merge":
			return "duplicate", true
		}
	case "triaged":
		switch input {
		case "start":
			return "in_progress", true
		case "hold":
			return "on_hold", true
		case "reject":
			return "rejected", true
		case "merge":
			return "duplicate", true
		}
	case "in_progress":
		switch input {
		case "hold":
			return "on_hold", true
		case "resolve":
			return "resolved", true
		case "reject":
			return "rejected", true
		case "merge":
			return "duplicate", true
		}
	case "on_hold":
		switch input {
		case "resume":
			return "in_progress", true
		case "reject":
			return "rejected", true
		}
	case "resolved":
		switch input {
		case "close":
			return "closed", true
		case "reject":
			return "rejected", true
		}
	}
	return "", false
}

func TestIssues_E2E_SeamAlignment(t *testing.T) {
	h := setupIssuesE2E(t)

	// 1. T8 + T9 + T10: Alta/high issue with reporter_id=77 and Media/medium issue without reporter.
	alta := e2eSuccess(t, h.call(t, "issues.CreateIssue", map[string]any{
		"tenant":      "tenant-alpha",
		"title":       "Compressor vibration",
		"description": "Unit 200 bearing noise",
		"priority":    "high",
		"severity":    "major",
		"asset_id":    200,
		"reporter_id": 77,
	}))
	altaID := int64(alta["id"].(float64))
	if alta["asset_category"] != "critico" || alta["channel"] != "api" || alta["reported_by"] != float64(77) || alta["assignee"] != float64(77) {
		t.Fatalf("unexpected Alta E2E issue: %v", alta)
	}
	aCreated, _ := time.Parse(time.RFC3339, alta["created_at"].(string))
	aFirst, _ := time.Parse(time.RFC3339, alta["sla_first_response_deadline"].(string))
	aRes, _ := time.Parse(time.RFC3339, alta["sla_resolution_deadline"].(string))
	if aFirst.Sub(aCreated) != 120*time.Minute || aRes.Sub(aCreated) != 1440*time.Minute {
		t.Errorf("Alta/high SLA = (%v, %v), want (120m, 1440m)", aFirst.Sub(aCreated), aRes.Sub(aCreated))
	}

	media := e2eSuccess(t, h.call(t, "issues.CreateIssue", map[string]any{
		"tenant":   "tenant-alpha",
		"title":    "Minor pump rattle",
		"priority": "medium",
		"severity": "minor",
		"asset_id": 201,
	}))
	mediaID := int64(media["id"].(float64))
	if media["asset_category"] != "normal" {
		t.Errorf("Media asset_category = %v, want normal", media["asset_category"])
	}
	mCreated, _ := time.Parse(time.RFC3339, media["created_at"].(string))
	mFirst, _ := time.Parse(time.RFC3339, media["sla_first_response_deadline"].(string))
	mRes, _ := time.Parse(time.RFC3339, media["sla_resolution_deadline"].(string))
	if mFirst.Sub(mCreated) != 480*time.Minute || mRes.Sub(mCreated) != 4320*time.Minute {
		t.Errorf("Media/medium SLA = (%v, %v), want (480m, 4320m)", mFirst.Sub(mCreated), mRes.Sub(mCreated))
	}

	// T7 + T14: single seam_issue_lifecycle definition across multiple CreateIssue calls, no tenant in stored doc.
	h.xolu.mu.Lock()
	defPosts := h.xolu.defPosts
	defName, _ := h.xolu.def["name"].(string)
	_, hasTenant := h.xolu.entities["issues"][altaID]["tenant"]
	h.xolu.mu.Unlock()
	if defPosts != 1 || defName != issues.DefinitionName {
		t.Fatalf("FSM definitions: posts=%d name=%q, want 1/%q", defPosts, defName, issues.DefinitionName)
	}
	if hasTenant {
		t.Errorf("stored xolu issue document must not include tenant key")
	}

	// 2. T7: triage -> merge -> duplicate.
	_ = e2eSuccess(t, h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant": "tenant-alpha", "issue_id": mediaID, "transition": "triage", "actor_id": 77,
	}))
	merged := e2eSuccess(t, h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant": "tenant-alpha", "issue_id": mediaID, "transition": "merge", "actor_id": 77,
	}))
	if merged["new_state"] != "duplicate" {
		t.Fatalf("merge new_state = %v, want duplicate", merged["new_state"])
	}

	// 3. T11: reject without reason fails; reject with reason sends {motivo, actor} and persists reject_reason.
	rejectMissingReason := e2eErrorText(t, h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant": "tenant-alpha", "issue_id": altaID, "transition": "reject",
	}))
	if !strings.Contains(rejectMissingReason, "reason") {
		t.Errorf("expected reason error, got %q", rejectMissingReason)
	}
	rejected := e2eSuccess(t, h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant":     "tenant-alpha",
		"issue_id":   altaID,
		"transition": "reject",
		"reason":     "False sensor spike",
		"actor_id":   77,
	}))
	rejIssue, _ := rejected["issue"].(map[string]any)
	if rejected["new_state"] != "rejected" || rejIssue["reject_reason"] != "False sensor spike" {
		t.Fatalf("unexpected reject response: %v", rejected)
	}
	h.xolu.mu.Lock()
	walkMotivo := h.xolu.lastWalkPayload["motivo"]
	walkActor := h.xolu.lastWalkPayload["actor"]
	h.xolu.mu.Unlock()
	if walkMotivo != "False sensor spike" || walkActor != float64(77) {
		t.Errorf("walk payload = (motivo=%v, actor=%v), want (False sensor spike, 77)", walkMotivo, walkActor)
	}

	// 4. T12: grouped child (master_id > 0) rejected with XOLU-INVALID_TRANSITION (grouped_child).
	child := e2eSuccess(t, h.call(t, "issues.CreateIssue", map[string]any{
		"tenant": "tenant-alpha", "title": "Child grouped symptom",
	}))
	childID := int64(child["id"].(float64))
	h.xolu.mu.Lock()
	h.xolu.entities["issues"][childID]["master_id"] = mediaID
	h.xolu.mu.Unlock()

	gotChild := e2eSuccess(t, h.call(t, "issues.GetIssue", map[string]any{
		"tenant": "tenant-alpha", "issue_id": childID,
	}))
	if gotChild["master_id"] != float64(mediaID) {
		t.Fatalf("GetIssue master_id = %v, want %d", gotChild["master_id"], mediaID)
	}

	groupedErr := e2eErrorText(t, h.call(t, "issues.AdvanceIssue", map[string]any{
		"tenant": "tenant-alpha", "issue_id": childID, "transition": "start", "actor_id": 77,
	}))
	if !strings.Contains(groupedErr, "XOLU-INVALID_TRANSITION") || !strings.Contains(groupedErr, "grouped_child") {
		t.Fatalf("expected XOLU-INVALID_TRANSITION grouped_child, got %q", groupedErr)
	}
}
