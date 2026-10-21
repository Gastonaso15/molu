package providers

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
)

func sampleRules() map[string][]string {
	return map[string][]string{
		"acme:operator": {"walk", "work_orders.CreateOrder"},
		"acme:viewer":   {"inventory.ReserveStock"},
		"globex:admin":  {"walk", "work_orders.CreateOrder", "inventory.ReserveStock"},
	}
}

func names(tools []rpi.ToolCandidate) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return out
}

func TestParseStaticContextRules(t *testing.T) {
	rules, err := ParseStaticContextRules([]byte(`{
		"acme:operator": ["walk", "work_orders.CreateOrder"],
		"acme:viewer": []
	}`))
	if err != nil {
		t.Fatalf("ParseStaticContextRules error: %v", err)
	}
	if got := rules["acme:operator"]; !slices.Equal(got, []string{"walk", "work_orders.CreateOrder"}) {
		t.Errorf(`rules["acme:operator"] = %v`, got)
	}
	if got, ok := rules["acme:viewer"]; !ok || len(got) != 0 {
		t.Errorf(`rules["acme:viewer"] = %v, %v; want empty, true`, got, ok)
	}
}

func TestParseStaticContextRulesNull(t *testing.T) {
	rules, err := ParseStaticContextRules([]byte(`null`))
	if err != nil {
		t.Fatalf("ParseStaticContextRules(null) error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("ParseStaticContextRules(null) = %v, want no rules", rules)
	}
}

func TestParseStaticContextRulesInvalid(t *testing.T) {
	for name, data := range map[string]string{
		"malformed":    `{"acme:operator": [`,
		"not object":   `["walk"]`,
		"wrong type":   `{"acme:operator": "walk"}`,
		"no separator": `{"acme": ["walk"]}`,
		"empty tenant": `{":operator": ["walk"]}`,
		"empty role":   `{"acme:": ["walk"]}`,
		"empty tool":   `{"acme:operator": ["walk", ""]}`,
	} {
		if _, err := ParseStaticContextRules([]byte(data)); err == nil {
			t.Errorf("%s: ParseStaticContextRules(%s) error = nil, want error", name, data)
		}
	}
}

func TestStaticRulesAdmitAllowedOnly(t *testing.T) {
	p := NewStaticContextRulesProvider(sampleRules())

	admitted, err := p.Admit(context.Background(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}, sampleCatalog())
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}
	// Catalog order is kept; primitives and namespaced functions both match.
	if want := []string{"walk", "work_orders.CreateOrder"}; !slices.Equal(names(admitted), want) {
		t.Errorf("Admit = %v, want %v", names(admitted), want)
	}
}

func TestStaticRulesAdmitUnionOfRoles(t *testing.T) {
	p := NewStaticContextRulesProvider(sampleRules())

	admitted, err := p.Admit(context.Background(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"viewer", "operator"}}, sampleCatalog())
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}
	if want := []string{"walk", "work_orders.CreateOrder", "inventory.ReserveStock"}; !slices.Equal(names(admitted), want) {
		t.Errorf("Admit = %v, want %v", names(admitted), want)
	}
}

func TestStaticRulesAdmitRejects(t *testing.T) {
	tests := []struct {
		name  string
		rules map[string][]string
		actx  rpi.AdmitContext
		tool  string
	}{
		{"tool not listed for role", sampleRules(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"viewer"}}, "walk"},
		{"other tenant", sampleRules(), rpi.AdmitContext{Tenant: "initech", Roles: []string{"operator"}}, "walk"},
		{"role from another tenant", sampleRules(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"admin"}}, "walk"},
		{"no roles", sampleRules(), rpi.AdmitContext{Tenant: "acme"}, "walk"},
		{"empty tenant", sampleRules(), rpi.AdmitContext{Tenant: "", Roles: []string{"operator"}}, "walk"},
		{"no rules", nil, rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}, "walk"},
		{"tenant case differs", sampleRules(), rpi.AdmitContext{Tenant: "ACME", Roles: []string{"operator"}}, "walk"},
		{"role case differs", sampleRules(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"Operator"}}, "walk"},
		{"tool case differs", map[string][]string{"acme:operator": {"Walk"}}, rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}, "walk"},
		{"namespace missing", map[string][]string{"acme:operator": {"CreateOrder"}}, rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}, "work_orders.CreateOrder"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStaticContextRulesProvider(tt.rules)
			admitted, err := p.Admit(context.Background(), tt.actx, sampleCatalog())
			if err != nil {
				t.Fatalf("Admit error: %v", err)
			}
			if slices.Contains(names(admitted), tt.tool) {
				t.Errorf("Admit(%+v) = %v, must not contain %q", tt.actx, names(admitted), tt.tool)
			}
		})
	}
}

func TestStaticRulesAdmitDeniedIsEmptyNonNil(t *testing.T) {
	p := NewStaticContextRulesProvider(nil)

	for name, catalog := range map[string][]rpi.ToolCandidate{"nil": nil, "empty": {}, "full": sampleCatalog()} {
		admitted, err := p.Admit(context.Background(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}, catalog)
		if err != nil {
			t.Fatalf("%s catalog: Admit error: %v", name, err)
		}
		if admitted == nil || len(admitted) != 0 {
			t.Errorf("%s catalog: Admit = %#v, want empty non-nil slice", name, admitted)
		}
	}
}

func TestStaticRulesDoesNotShareRules(t *testing.T) {
	rules := sampleRules()
	p := NewStaticContextRulesProvider(rules)

	// Changing the caller's rules after construction must not change the provider.
	rules["acme:operator"][0] = "find"
	rules["acme:viewer"] = append(rules["acme:viewer"], "walk")

	actx := rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}
	admitted, _ := p.Admit(context.Background(), actx, sampleCatalog())
	if !slices.Contains(names(admitted), "walk") {
		t.Errorf("Admit(operator) = %v, want it to still contain walk", names(admitted))
	}
	actx.Roles = []string{"viewer"}
	admitted, _ = p.Admit(context.Background(), actx, sampleCatalog())
	if slices.Contains(names(admitted), "walk") {
		t.Errorf("Admit(viewer) = %v, must not contain walk", names(admitted))
	}
}

func TestStaticRulesScore(t *testing.T) {
	p := NewStaticContextRulesProvider(sampleRules())
	actx := rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}

	// inventory.ReserveStock is not allowed for acme:operator, so it scores 0.0
	// and goes last; allowed tools keep their relative order.
	input := []rpi.ToolCandidate{
		{Name: "inventory.ReserveStock", Namespace: "inventory"},
		{Name: "work_orders.CreateOrder", Namespace: "work_orders"},
		{Name: "walk", Primitive: true},
	}
	scored, err := p.Score(context.Background(), actx, input)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}

	want := []struct {
		name  string
		score float64
	}{
		{"work_orders.CreateOrder", 1.0},
		{"walk", 1.0},
		{"inventory.ReserveStock", 0.0},
	}
	if len(scored) != len(want) {
		t.Fatalf("Score = %d tools, want %d", len(scored), len(want))
	}
	for i, w := range want {
		if scored[i].Name != w.name || scored[i].Score != w.score {
			t.Errorf("Score[%d] = %q %.1f, want %q %.1f", i, scored[i].Name, scored[i].Score, w.name, w.score)
		}
	}
}

func TestStaticRulesScoreEmpty(t *testing.T) {
	p := NewStaticContextRulesProvider(sampleRules())

	scored, err := p.Score(context.Background(), rpi.AdmitContext{Tenant: "acme"}, nil)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}
	if scored == nil || len(scored) != 0 {
		t.Errorf("Score(nil) = %#v, want empty non-nil slice", scored)
	}
}

func TestStaticRulesStoresTelemetry(t *testing.T) {
	p := NewStaticContextRulesProvider(nil)
	logger := telemetry.NewLogger(telemetry.DefaultConfig())
	metrics := telemetry.NewMetricsWithRegistry(prometheus.NewRegistry())

	p.SetLogger(logger)
	p.SetMetrics(metrics)

	if p.Logger() != logger {
		t.Error("Logger() did not return the logger set with SetLogger")
	}
	if p.Metrics() != metrics {
		t.Error("Metrics() did not return the metrics set with SetMetrics")
	}
}

func TestStaticRulesConcurrent(t *testing.T) {
	p := NewStaticContextRulesProvider(sampleRules())
	catalog := sampleCatalog()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tenant := []string{"acme", "globex"}[i%2]
			role := []string{"operator", "admin"}[i%2]
			actx := rpi.AdmitContext{Tenant: tenant, Roles: []string{role}, Query: fmt.Sprintf("q-%d", i)}
			admitted, err := p.Admit(context.Background(), actx, catalog)
			if err != nil {
				t.Errorf("Admit error: %v", err)
				return
			}
			if _, err := p.Score(context.Background(), actx, admitted); err != nil {
				t.Errorf("Score error: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
