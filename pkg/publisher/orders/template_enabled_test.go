package orders

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func ruleOf(err error) string {
	var te *TransitionError
	if errors.As(err, &te) {
		r, _ := te.Details[workflowRuleKey].(string)
		return r
	}
	return ""
}

func TestSetTemplateEnabled_DisableBlocksCreateOrderAndEnableRestores(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)

	in := validTemplateInput()
	in.HeaderItems = nil
	in.Blocks = nil
	in.ConsumablesEnabled = false
	in.Consumables = nil
	in.Actions = nil
	in.MinAssets, in.MaxAssets, in.AssetTypeID = 0, 0, ""
	tpl, err := svc.CreateTemplate(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !tpl.Enabled {
		t.Fatalf("a new template must be enabled: %+v", tpl)
	}
	fs.entities[TemplateEntityType][tpl.ID]["_version"] = int64(1)

	res, err := svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: ID(tpl.ID), Enabled: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Template.Enabled {
		t.Fatalf("res = %+v", res)
	}
	if got := fs.entities[TemplateEntityType][tpl.ID]["enabled"]; got != false {
		t.Fatalf("stored enabled = %v", got)
	}

	// ListTemplates and GetTemplate report it.
	list, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	if err != nil || len(list.Templates) != 1 || list.Templates[0].Enabled {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	got, err := svc.GetTemplate(ctx, GetTemplateInput{TemplateID: ID(tpl.ID)})
	if err != nil || got.Enabled {
		t.Fatalf("get = %+v err=%v", got, err)
	}

	// A disabled template cannot open orders.
	_, err = svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl.ID), Title: "x"})
	if ruleOf(err) != "template_disabled" {
		t.Fatalf("CreateOrder on disabled template: err = %v", err)
	}
	if n := len(fs.entities[EntityType]); n != 0 {
		t.Fatalf("%d orders were written for a disabled template", n)
	}

	// Enabling restores it.
	res, err = svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: ID(tpl.ID), Enabled: boolPtr(true)})
	if err != nil || !res.Changed || !res.Template.Enabled {
		t.Fatalf("enable: res = %+v err=%v", res, err)
	}
	if _, err = svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl.ID), Title: "x", AssignmentType: AssignmentBoard, ResponsibleType: "crew"}); err != nil {
		t.Fatalf("CreateOrder after enabling: %v", err)
	}
}

func TestSetTemplateEnabled_Rules(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)
	v1, err := svc.CreateTemplate(ctx, validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[TemplateEntityType][v1.ID]["_version"] = int64(1)

	// Same state: nothing is written and the version does not move.
	res, err := svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: ID(v1.ID), Enabled: boolPtr(true)})
	if err != nil || res.Changed {
		t.Fatalf("idempotent: res = %+v err=%v", res, err)
	}
	if fs.entities[TemplateEntityType][v1.ID]["_version"] != int64(1) {
		t.Fatal("an unchanged state must not write")
	}

	// A template saved before the flag existed reads as enabled and can be disabled.
	delete(fs.entities[TemplateEntityType][v1.ID], "enabled")
	list, _ := svc.ListTemplates(ctx, ListTemplatesInput{})
	if len(list.Templates) != 1 || !list.Templates[0].Enabled {
		t.Fatalf("missing flag must read as enabled: %+v", list)
	}
	if res, err = svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: ID(v1.ID), Enabled: boolPtr(false)}); err != nil || !res.Changed {
		t.Fatalf("disable legacy doc: res=%+v err=%v", res, err)
	}

	// Contract errors.
	for name, in := range map[string]SetTemplateEnabledInput{
		"missing enabled": {TemplateID: ID(v1.ID)},
		"bad id":          {TemplateID: "abc", Enabled: boolPtr(true)},
	} {
		if _, err := svc.SetTemplateEnabled(ctx, in); err == nil {
			t.Errorf("%s: expected a validation error", name)
		} else {
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: err = %T %v", name, err, err)
			}
		}
	}
	var nf *NotFoundError
	if _, err := svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: "999", Enabled: boolPtr(true)}); !errors.As(err, &nf) {
		t.Errorf("unknown template: err = %v", err)
	}

	// Only the latest version can be switched.
	v2, err := svc.PublishTemplateVersion(ctx, PublishTemplateVersionInput{PreviousTemplateID: ID(v1.ID), CreateTemplateInput: validTemplateInput()})
	if err != nil {
		t.Fatal(err)
	}
	if !v2.Enabled {
		t.Fatalf("a new version starts enabled: %+v", v2)
	}
	_, err = svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: ID(v1.ID), Enabled: boolPtr(true)})
	if ruleOf(err) != "not_latest" {
		t.Fatalf("superseded version: err = %v", err)
	}
}

func TestSetTemplateEnabled_StaleVersion(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)
	tpl, err := svc.CreateTemplate(ctx, validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[TemplateEntityType][tpl.ID]["_version"] = int64(1)
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	_, err = svc.SetTemplateEnabled(ctx, SetTemplateEnabledInput{TemplateID: ID(tpl.ID), Enabled: boolPtr(false)})
	if ruleOf(err) != "stale_state" {
		t.Fatalf("err = %v", err)
	}
}

func TestSetTemplateEnabled_HTTP(t *testing.T) {
	fs := newFake()
	seedTemplateWrite(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	tpl, err := NewService(fs).CreateTemplate(context.Background(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[TemplateEntityType][tpl.ID]["_version"] = int64(1)

	post := func(body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/orders/templates/set-enabled", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	if r := post(`{"template_id":"` + tpl.ID + `","enabled":false}`); r.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", r.StatusCode)
	}
	if r := post(`{"template_id":"` + tpl.ID + `"}`); r.StatusCode != http.StatusBadRequest && r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("missing enabled: status = %d", r.StatusCode)
	}
}

func TestCreateOrder_RefusesASupersededTemplateVersion(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)
	in := validTemplateInput()
	in.HeaderItems, in.Blocks, in.ConsumablesEnabled, in.Consumables, in.Actions = nil, nil, false, nil, nil
	in.MinAssets, in.MaxAssets, in.AssetTypeID = 0, 0, ""
	v1, err := svc.CreateTemplate(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[TemplateEntityType][v1.ID]["_version"] = int64(1)
	open := func(id string) error {
		_, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(id), Title: "x", AssignmentType: AssignmentBoard, ResponsibleType: "crew"})
		return err
	}
	if err := open(v1.ID); err != nil {
		t.Fatalf("the latest version opens orders: %v", err)
	}
	v2, err := svc.PublishTemplateVersion(ctx, PublishTemplateVersionInput{PreviousTemplateID: ID(v1.ID), CreateTemplateInput: in})
	if err != nil {
		t.Fatal(err)
	}
	orders := len(fs.entities[EntityType])
	if err := open(v1.ID); ruleOf(err) != "template_not_latest" {
		t.Fatalf("the superseded version: err = %v", err)
	}
	if len(fs.entities[EntityType]) != orders || len(fs.machines) != 1 {
		t.Fatal("a refused order wrote something")
	}
	if err := open(v2.ID); err != nil {
		t.Fatalf("the new version opens orders: %v", err)
	}
}
