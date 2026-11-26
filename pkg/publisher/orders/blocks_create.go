package orders

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// blockPredecessors validates a template's blocks and turns each block's
// predecessor name into an index (-1 for none), as Seam does: names are
// compared ignoring case and surrounding spaces; several blocks may share a
// predecessor or have none; a predecessor may come later in the list. Empty or
// repeated names, unknown predecessors, a block that waits for itself and
// dependency cycles are rejected.
func blockPredecessors(blocks []TemplateBlock) ([]int, error) {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	index := make(map[string]int, len(blocks))
	for i, b := range blocks {
		name := norm(b.Name)
		if name == "" {
			return nil, fmt.Errorf("block %d has an empty name", i)
		}
		if prev, dup := index[name]; dup {
			return nil, fmt.Errorf("blocks %d and %d have the same name", prev, i)
		}
		index[name] = i
	}
	preds := make([]int, len(blocks))
	for i, b := range blocks {
		preds[i] = -1
		name := norm(b.Predecessor)
		if name == "" {
			continue
		}
		p, ok := index[name]
		if !ok {
			return nil, fmt.Errorf("block %q references unknown predecessor %q", b.Name, b.Predecessor)
		}
		if p == i {
			return nil, fmt.Errorf("block %q references itself", b.Name)
		}
		preds[i] = p
	}
	const (
		unseen = iota
		visiting
		done
	)
	state := make([]uint8, len(blocks))
	var visit func(int) error
	visit = func(i int) error {
		switch state[i] {
		case visiting:
			return fmt.Errorf("dependency cycle involving block %q", blocks[i].Name)
		case done:
			return nil
		}
		state[i] = visiting
		if p := preds[i]; p >= 0 {
			if err := visit(p); err != nil {
				return err
			}
		}
		state[i] = done
		return nil
	}
	for i := range blocks {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return preds, nil
}

// blockDefinitionID resolves the shared block lifecycle once. A failed attempt
// is not cached.
func (s *Service) blockDefinitionID(ctx context.Context) (string, error) {
	s.blockDefMu.Lock()
	defer s.blockDefMu.Unlock()
	if s.blockDefID != "" {
		return s.blockDefID, nil
	}
	id, err := s.sub.EnsureDefinition(ctx, BlockDefinitionName, BlockLifecycleDefinition())
	if err != nil {
		return "", err
	}
	s.blockDefID = id
	return id, nil
}

// commitRejected lists the statuses that mean xolu refused the request: the
// commit did not happen, so the machines made for it can be removed.
func commitRejected(status int) bool {
	switch status {
	case 400, 401, 403, 404, 409, 413, 422:
		return true
	}
	return false
}

// createOrderWithBlocksAndWarehouse stores a template order together with its
// optional block records and optional mobile warehouse in one atomic Commit.
//
// Machines are separate API calls, so a failure before the commit deletes the
// ones already made. A commit xolu rejects deletes them too. A commit whose
// outcome is unknown (timeout, server error) is checked by reading the order
// back with a token unique to this attempt: if it is there the creation
// succeeded; if not, the machines are kept (the commit may still land) and the
// error says which order may exist.
func (s *Service) createOrderWithBlocksAndWarehouse(ctx context.Context, tpl workOrderTemplate, doc map[string]any, createdByUserID int64, blockResp []int64) (string, error) {
	hasBlocks := len(tpl.Blocks) > 0
	hasWarehouse := tpl.UsesConsumables

	var preds []int
	if hasBlocks {
		var err error
		preds, err = blockPredecessors(tpl.Blocks)
		if err != nil {
			return "", &ValidationError{Fields: []string{"template_id"}, Reason: "invalid block dependencies in the template: " + err.Error()}
		}
	}
	appendCount := len(tpl.Blocks)
	if hasWarehouse {
		appendCount++
	}
	if appendCount > maxCommitAppends {
		return "", &ValidationError{Fields: []string{"template_id"},
			Reason: fmt.Sprintf("the template needs %d commit records (blocks + mobile warehouse) but one commit holds at most %d records", appendCount, maxCommitAppends)}
	}

	var defID string
	if hasBlocks {
		var err error
		defID, err = s.blockDefinitionID(ctx)
		if err != nil {
			return "", classify(err, "")
		}
	}
	orderID, token, err := s.newOrderIdentity(ctx, hasWarehouse)
	if err != nil {
		return "", err
	}

	// Every template order has its own machine of the template version's
	// workflow definition; the order's state moves with it.
	wfDefID, err := s.sub.EnsureDefinition(ctx, TemplateDefinitionName(tpl.ID, tpl.Version), TemplateWorkflowDefinition(tpl.ID, tpl.Version, tpl.States))
	if err != nil {
		return "", classify(err, "")
	}
	wf, err := s.sub.CreateMachine(ctx, wfDefID)
	if err != nil {
		return "", classify(err, "")
	}
	machineIDs := []string{wf.ID}
	if wf.State != tpl.States[0] {
		s.discardMachines(ctx, machineIDs)
		return "", fmt.Errorf("the workflow machine started in state %q, not %q", wf.State, tpl.States[0])
	}
	doc["machine_id"] = numericOrString(wf.ID)

	// xolu refuses a Commit with nothing to append and no machine step, so an
	// order with no blocks and no mobile warehouse is a plain create: there
	// are no related records to save with it.
	if appendCount == 0 {
		return s.createOrderPlain(ctx, doc, token, machineIDs)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	appends := make([]CommitAppend, 0, appendCount)
	for i, b := range tpl.Blocks {
		ref := fmt.Sprintf("work_order:%d:block:%d", orderID, i)
		m, err := s.sub.CreateMachineWithRef(ctx, defID, ref)
		if err != nil {
			s.discardMachines(ctx, machineIDs)
			return "", classify(err, "")
		}
		machineIDs = append(machineIDs, m.ID)
		if m.State != "pending" {
			s.discardMachines(ctx, machineIDs)
			return "", fmt.Errorf("the machine of block %d started in state %q, not pending", i, m.State)
		}
		appends = append(appends, CommitAppend{Entity: workOrderBlocksEntity, Data: map[string]any{
			"work_order_id":     orderID,
			"template_id":       intOf(tpl.ID),
			"template_version":  tpl.Version,
			"block_index":       i,
			"predecessor_index": preds[i],
			"name":              strings.TrimSpace(b.Name),
			"machine_id":        numericOrString(m.ID),
			"state":             m.State,
			"responsible_type":  strings.TrimSpace(b.ResponsibleType),
			"responsible_id":    blockResp[i],
			"values":            map[string]any{},
			"started_at":        "",
			"completed_at":      "",
			"created_at":        now,
			"updated_at":        now,
		}})
	}

	data := make(map[string]any, len(doc)+4)
	for k, v := range doc {
		data[k] = v
	}
	data["creation_token"] = token
	data["block_execution_version"] = 1
	data["block_count"] = len(tpl.Blocks)

	if hasWarehouse {
		// IDs are scoped by entity, so the mobile warehouse shares the order ID.
		data["mobile_warehouse_id"] = orderID
		doc["mobile_warehouse_id"] = orderID
		title, _ := doc["title"].(string)
		warehouseData := map[string]any{
			"name":          title + " - " + warehouseDate(),
			"type":          "mobile",
			"work_order_id": orderID,
			"enabled":       true,
			"consumables":   []any{},
		}
		if createdByUserID > 0 {
			warehouseData["responsible_user_id"] = createdByUserID
		}
		whID := orderID
		appends = append(appends, CommitAppend{
			Entity: warehouseEntity,
			ID:     &whID,
			Data:   warehouseData,
		})
	}

	commitErr := s.sub.Commit(ctx, CommitRequest{
		Update: CommitUpdate{Entity: EntityType, ID: orderID, Data: data},
		Append: appends,
	})
	id := strconv.FormatInt(orderID, 10)
	if commitErr == nil {
		return id, nil
	}
	var xe *XoluError
	if errors.As(commitErr, &xe) && commitRejected(xe.Status) {
		s.discardMachines(ctx, machineIDs)
		return "", fmt.Errorf("commit work order and related records: %w", commitErr)
	}
	// Unknown outcome: look for this attempt's order with a context that
	// survives the caller's cancellation.
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if saved, err := s.sub.GetEntity(check, EntityType, id); err == nil && saved["creation_token"] == token {
		return id, nil
	}
	return "", &UnavailableError{Cause: fmt.Errorf("could not confirm the creation of work order %s (it may have been created: check it with GetOrder before retrying): %w", id, commitErr)}
}

// newOrderIdentity picks an order id that does not exist yet (and is also free
// in the warehouse entity when opening a mobile warehouse) and a token that
// identifies this creation attempt. Ids stay within JavaScript's exact integer
// range.
func (s *Service) newOrderIdentity(ctx context.Context, checkWarehouse bool) (int64, string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, "", fmt.Errorf("generate the order identity: %w", err)
		}
		id := int64(binary.BigEndian.Uint64(raw[:8])&((1<<52)-1)) + 1
		token := hex.EncodeToString(raw[8:])
		idStr := strconv.FormatInt(id, 10)
		// A Commit's primary write is an upsert: refuse an id that is taken.
		_, err := s.sub.GetEntity(ctx, EntityType, idStr)
		switch {
		case err == nil:
			continue
		case !isMissing(err):
			return 0, "", classify(err, "")
		}
		if checkWarehouse {
			_, werr := s.sub.GetEntity(ctx, warehouseEntity, idStr)
			switch {
			case werr == nil:
				continue
			case !isMissing(werr):
				return 0, "", classify(werr, "")
			}
		}
		return id, token, nil
	}
	return 0, "", errors.New("could not find a free work order id")
}

// discardMachines removes machines made for a creation that did not happen.
// It is best effort and survives the caller's cancellation.
func (s *Service) discardMachines(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	for _, id := range ids {
		_ = s.sub.DeleteMachine(cleanup, id)
	}
}

// warehouseDate is the date the mobile warehouse's name carries (dd-mm-yyyy,
// Montevideo time), as the work order module names it.
func warehouseDate() string {
	loc, err := time.LoadLocation("America/Montevideo")
	if err != nil {
		loc = time.FixedZone("UTC-3", -3*60*60)
	}
	return time.Now().In(loc).Format("02-01-2006")
}

// blockResponsibles resolves who runs each block. Every block needs one, of
// the type its template block names (user, crew or third_party); the type
// always comes from the template, never from the request.
func (s *Service) blockResponsibles(ctx context.Context, tpl workOrderTemplate, given map[string]ID) ([]int64, []fieldProblem, error) {
	out := make([]int64, len(tpl.Blocks))
	if len(tpl.Blocks) > 0 {
		if _, err := blockPredecessors(tpl.Blocks); err != nil {
			return nil, nil, &ValidationError{Fields: []string{"template_id"}, Reason: "invalid block dependencies in the template: " + err.Error()}
		}
		n := len(tpl.Blocks)
		if tpl.UsesConsumables {
			n++
		}
		if n > maxCommitAppends {
			return nil, nil, &ValidationError{Fields: []string{"template_id"},
				Reason: fmt.Sprintf("the template needs %d commit records (blocks + mobile warehouse) but one commit holds at most %d records", n, maxCommitAppends)}
		}
	}
	var problems []fieldProblem
	known := map[string]bool{}
	for _, b := range tpl.Blocks {
		known[normName(b.Name)] = true
	}
	names := make([]string, 0, len(given))
	for name := range given {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !known[normName(name)] {
			problems = append(problems, fieldProblem{"block_responsibles." + name, "not a block of this template"})
		}
	}
	for i, b := range tpl.Blocks {
		field := "block_responsibles." + strings.TrimSpace(b.Name)
		var raw ID
		for name, id := range given {
			if normName(name) == normName(b.Name) {
				raw = id
			}
		}
		id, ok := parsePositiveID(raw)
		if !ok {
			problems = append(problems, fieldProblem{field, "required: the id of a " + strings.TrimSpace(b.ResponsibleType)})
			continue
		}
		if _, err := s.resolveDirectResponsible(ctx, strings.TrimSpace(b.ResponsibleType), id); err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				problems = append(problems, fieldProblem{field, ve.Reason})
				continue
			}
			return nil, nil, err
		}
		out[i] = id
	}
	return out, problems, nil
}

// createOrderPlain stores an order that has no related records and lets xolu
// assign its id. A rejected create removes the order's machine; one whose
// outcome is unknown is looked up by this attempt's token before the machine
// is given up on.
func (s *Service) createOrderPlain(ctx context.Context, doc map[string]any, token string, machineIDs []string) (string, error) {
	data := make(map[string]any, len(doc)+3)
	for k, v := range doc {
		data[k] = v
	}
	data["creation_token"] = token
	data["block_execution_version"] = 1
	data["block_count"] = 0
	id, err := s.sub.CreateEntity(ctx, EntityType, data)
	if err == nil {
		return id, nil
	}
	var xe *XoluError
	if errors.As(err, &xe) && commitRejected(xe.Status) {
		s.discardMachines(ctx, machineIDs)
		return "", classify(err, "")
	}
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	rows, ferr := s.sub.FindEntities(check, EntityType, EntityQuery{
		Where: []Condition{{Field: "creation_token", Op: "=", Value: token}}, Limit: 1,
	})
	if ferr == nil && len(rows) == 1 {
		return idString(rows[0]["id"]), nil
	}
	if ferr == nil || isMissing(ferr) {
		// xolu answered and the order is not there: nothing was created.
		s.discardMachines(ctx, machineIDs)
		return "", classify(err, "")
	}
	return "", &UnavailableError{Cause: fmt.Errorf("could not confirm the creation of the work order (it may have been created: look for creation_token %s with ListOrders before retrying): %w", token, err)}
}
