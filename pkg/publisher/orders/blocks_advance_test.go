package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func advBlocks() []any {
	return []any{
		map[string]any{"name": "Preparacion", "responsible_type": "user", "lines": []any{
			map[string]any{"name": "aislado", "type": "boolean"},
			map[string]any{"name": "nota", "type": "text"},
		}},
		map[string]any{"name": "Trabajo", "responsible_type": "crew", "predecessor": "Preparacion", "lines": []any{
			map[string]any{"name": "horas", "type": "number"},
			map[string]any{"name": "fecha", "type": "date"},
		}},
		map[string]any{"name": "Cierre", "responsible_type": "crew", "predecessor": "Trabajo", "lines": []any{}},
	}
}

// newBlockOrder opens an order from a three-block template with three states.
func newBlockOrder(t *testing.T) (*fakeSubstrate, *Service, string) {
	t.Helper()
	fs, svc := newBlockWorld(t, advBlocks())
	o, err := svc.CreateOrder(context.Background(), blockInput())
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[EntityType][o.ID]["_version"] = int64(1) // what xolu stamps on a stored document
	return fs, svc, o.ID
}

func stored(fs *fakeSubstrate, id string) map[string]any {
	bv, _ := fs.entities[EntityType][id]["block_values"].(map[string]any)
	return bv
}

func TestAdvanceOrder_WithBlocks_AdvancesWithoutAnswers(t *testing.T) {
	fs, svc, id := newBlockOrder(t)
	res, err := advance(svc, id, nil)
	if err != nil || res.Current != "En curso" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(stored(fs, id)) != 0 {
		t.Errorf("no answers were given: %v", stored(fs, id))
	}
}

func TestAdvanceOrder_WithBlocks_StoresAnswersUnderSeamKeys(t *testing.T) {
	fs, svc, id := newBlockOrder(t)
	_, err := advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"Preparacion": map[string]any{"aislado": true, "nota": "ok"}}
	})
	if err != nil {
		t.Fatal(err)
	}
	bv := stored(fs, id)
	if bv["template_block_0_line_0"] != true || bv["template_block_0_line_1"] != "ok" || len(bv) != 2 {
		t.Fatalf("stored = %v", bv)
	}
	// The next step adds answers and keeps the earlier ones; names ignore case and spaces.
	_, err = advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{" trabajo ": map[string]any{"HORAS": "12.5", "fecha": "2026-11-23"}}
		in.Payload = map[string]any{"technician_signature": "firma"}
	})
	if err != nil {
		t.Fatal(err)
	}
	bv = stored(fs, id)
	if bv["template_block_1_line_0"] != 12.5 || bv["template_block_1_line_1"] != "2026-11-23" || bv["template_block_0_line_0"] != true || len(bv) != 4 {
		t.Fatalf("stored = %v", bv)
	}
}

func TestAdvanceOrder_WithBlocks_AnAnswerCanBeReplacedAndBlankKeepsIt(t *testing.T) {
	fs, svc, id := newBlockOrder(t)
	if _, err := advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"Preparacion": map[string]any{"nota": "primera"}}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"Preparacion": map[string]any{"nota": "   ", "aislado": nil}}
		in.Payload = map[string]any{"technician_signature": "firma"}
	}); err != nil {
		t.Fatal(err)
	}
	if bv := stored(fs, id); bv["template_block_0_line_1"] != "primera" || len(bv) != 1 {
		t.Fatalf("blank answers must keep what is stored: %v", bv)
	}

	fs, svc, id = newBlockOrder(t)
	_, _ = advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"Preparacion": map[string]any{"nota": "vieja"}}
	})
	_, _ = advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"Preparacion": map[string]any{"nota": "nueva"}}
		in.Payload = map[string]any{"technician_signature": "firma"}
	})
	if bv := stored(fs, id); bv["template_block_0_line_1"] != "nueva" {
		t.Fatalf("stored = %v", bv)
	}
}

func TestAdvanceOrder_WithBlocks_InvalidAnswersChangeNothing(t *testing.T) {
	cases := map[string]map[string]any{
		"unknown block":       {"Z": map[string]any{"x": 1}},
		"unknown line":        {"Preparacion": map[string]any{"nope": 1}},
		"number as text":      {"Trabajo": map[string]any{"horas": "muchas"}},
		"bad date":            {"Trabajo": map[string]any{"fecha": "ayer"}},
		"bad boolean":         {"Preparacion": map[string]any{"aislado": "quizas"}},
		"block not an object": {"Preparacion": "si"},
	}
	for name, bv := range cases {
		t.Run(name, func(t *testing.T) {
			fs, svc, id := newBlockOrder(t)
			_, err := advance(svc, id, func(in *AdvanceInput) { in.BlockValues = bv })
			ve := wantInvalidPrefix(t, err, "block_values")
			if ve.Reason == "" {
				t.Error("the error should say what is wrong")
			}
			if fs.entities[EntityType][id]["state"] != "Pendiente" || len(stored(fs, id)) != 0 {
				t.Errorf("a rejected step must not change the order: %v", fs.entities[EntityType][id])
			}
		})
	}
}

func TestAdvanceOrder_WithBlocks_ReportsEveryProblemField(t *testing.T) {
	_, svc, id := newBlockOrder(t)
	_, err := advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{
			"Trabajo": map[string]any{"horas": "x", "fecha": "y"},
			"Z":       map[string]any{"a": 1},
		}
	})
	var ve *ValidationError
	if !asValidation(err, &ve) || len(ve.Fields) != 3 {
		t.Fatalf("err = %v fields=%v", err, ve)
	}
}

func TestAdvanceOrder_BlockValuesOnATemplateWithoutBlocks(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	svc := NewService(fs)
	o := mustCreate(t, svc, tplInput())
	_, err := advance(svc, o.ID, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"B": map[string]any{"x": 1}}
	})
	wantInvalidPrefix(t, err, "block_values")
}

func TestAdvanceOrder_StillRefusesOrdersWithAMobileWarehouse(t *testing.T) {
	fs, svc, id := newBlockOrder(t)
	fs.entities[EntityType][id]["mobile_warehouse_id"] = int64(5)
	_, err := advance(svc, id, nil)
	wantInvalid(t, err, "order_id")
}

func TestGetOrder_DecodesBlockAnswersByName(t *testing.T) {
	fs, svc, id := newBlockOrder(t)
	if _, err := advance(svc, id, func(in *AdvanceInput) {
		in.BlockValues = map[string]any{"Preparacion": map[string]any{"aislado": true}, "Trabajo": map[string]any{"horas": 3}}
	}); err != nil {
		t.Fatal(err)
	}
	d, err := svc.GetOrder(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if d.BlockAnswers["Preparacion"]["aislado"] != true || d.BlockAnswers["Trabajo"]["horas"] != 3.0 || len(d.BlockAnswers) != 2 {
		t.Fatalf("answers = %v", d.BlockAnswers)
	}
	// The template gone: the raw values still come back, without failing.
	delete(fs.entities[TemplateEntityType], "10")
	d, err = svc.GetOrder(context.Background(), id)
	if err != nil || d.BlockAnswers != nil || len(d.BlockValues) != 2 {
		t.Fatalf("detail = %+v err=%v", d, err)
	}
}

func TestOrderServer_AdvanceAcceptsBlockValues(t *testing.T) {
	fs, _, id := newBlockOrder(t)
	srv := newTestServer(fs)
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/advance", testToken,
		`{"order_id":"`+id+`","input":"complete","header_values":{"observacion":"ok"},"block_values":{"Preparacion":{"aislado":true}}}`)
	if resp.StatusCode != 200 || out["current"] != "En curso" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, out = post(t, srv.URL+"/api/v1/orders/advance", testToken,
		`{"order_id":"`+id+`","input":"complete","header_values":{"observacion":"ok"},"block_values":{"Z":{"a":1}}}`)
	if resp.StatusCode != 400 {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
}

func asValidation(err error, target **ValidationError) bool { return errors.As(err, target) }

// wantInvalidPrefix expects a ValidationError with a field under prefix
// (per-field problems are named like block_values.<block>.<line>).
func wantInvalidPrefix(t *testing.T, err error, prefix string) *ValidationError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError under %q, got %T %v", prefix, err, err)
	}
	for _, f := range ve.Fields {
		if f == prefix || strings.HasPrefix(f, prefix+".") {
			return ve
		}
	}
	t.Fatalf("fields %v are not under %q", ve.Fields, prefix)
	return ve
}
