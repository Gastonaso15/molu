package hub

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
)

const testRules = `{"t1:operator": ["walk"]}`

func admittedNames(t *testing.T, p rpi.RPIProvider, actx rpi.AdmitContext) []string {
	t.Helper()
	candidates := []rpi.ToolCandidate{{Name: "walk", Primitive: true}, {Name: "work_orders.CreateOrder", Namespace: "work_orders"}}
	admitted, err := p.Admit(context.Background(), actx, candidates)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, a := range admitted {
		out = append(out, a.Name)
	}
	return out
}

func TestNewRPIProviderAdmitAll(t *testing.T) {
	p, err := NewRPIProvider("admit-all", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*providers.AdmitAllProvider); !ok {
		t.Fatalf("expected *AdmitAllProvider, got %T", p)
	}
}

func TestNewRPIProviderStaticRulesInline(t *testing.T) {
	p, err := NewRPIProvider("static-context-rules", "", testRules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := admittedNames(t, p, rpi.AdmitContext{Tenant: "t1", Roles: []string{"operator"}})
	if !reflect.DeepEqual(got, []string{"walk"}) {
		t.Fatalf("expected rules to apply, got %v", got)
	}
}

func TestNewRPIProviderStaticRulesFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(testRules), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewRPIProvider("static-context-rules", path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := admittedNames(t, p, rpi.AdmitContext{Tenant: "t1", Roles: []string{"operator"}})
	if !reflect.DeepEqual(got, []string{"walk"}) {
		t.Fatalf("expected rules to apply, got %v", got)
	}
}

func TestNewRPIProviderErrors(t *testing.T) {
	cases := map[string]struct {
		name, file, inline string
		want               string
	}{
		"empty name":      {"", "", "", "provider"},
		"unknown name":    {"ml-ranker", "", "", "ml-ranker"},
		"static no rules": {"static-context-rules", "", "", "rules"},
		"missing file":    {"static-context-rules", filepath.Join(t.TempDir(), "nope.json"), "", "nope.json"},
		"invalid JSON":    {"static-context-rules", "", `{not json`, "invalid JSON"},
		"bad key":         {"static-context-rules", "", `{"nocolon":["walk"]}`, "tenant:role"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			p, err := NewRPIProvider(c.name, c.file, c.inline)
			if err == nil || p != nil {
				t.Fatalf("expected error and nil provider, got %v / %v", p, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error should mention %q, got %v", c.want, err)
			}
		})
	}
}
