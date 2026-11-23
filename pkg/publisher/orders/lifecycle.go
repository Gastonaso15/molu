package orders

// LifecycleDefinition is the FSM Seam's Work Orders module uses for orders
// created without a template (the "legacy" path):
//
//	draft --start--> in_progress --complete[technician_signature]--> completed
//
// It mirrors seam-CURE (internal/ui/workorders.go, ensureWorkOrderDefinition)
// so orders created through the Hub and orders created from Seam's UI share
// one definition. "complete" is guarded by the technician's signature, so a
// rejected transition has a guard diagnostic worth surfacing verbatim.
func LifecycleDefinition() Definition {
	return Definition{
		"name":        DefinitionName,
		"description": "Seam AMS Work Orders playground -- minimal lifecycle, not T-11's full Order Engine scope",
		"initial":     StateDraft,
		// xolu requires an explicit level (XOLU-FSM006 otherwise); Seam uses firstmatch.
		"determinism": "firstmatch",
		"states": map[string]any{
			StateDraft:      map[string]any{"terminal": false},
			StateInProgress: map[string]any{"terminal": false},
			StateCompleted:  map[string]any{"terminal": true},
		},
		"transitions": []any{
			map[string]any{"from": StateDraft, "input": InputStart, "to": StateInProgress},
			map[string]any{
				"from": StateInProgress, "input": InputComplete, "to": StateCompleted,
				// Single-quoted literal: a double-quoted token is an identifier in xolu's T-SQL lexer.
				"guard": "payload.technician_signature != ''",
			},
		},
	}
}

// BlockDefinitionName is the FSM definition every block of every order uses.
const BlockDefinitionName = "seam_work_order_block_lifecycle_v1"

// BlockLifecycleDefinition is the lifecycle of one work order block, as Seam's
// order creation defines it:
//
//	pending --start--> in_progress --complete--> completed
//
// Every block of every order gets its own machine of this definition.
func BlockLifecycleDefinition() Definition {
	return Definition{
		"name":        BlockDefinitionName,
		"description": "Work order block lifecycle",
		"initial":     "pending",
		"determinism": "firstmatch",
		"states": map[string]any{
			"pending":     map[string]any{"terminal": false},
			"in_progress": map[string]any{"terminal": false},
			"completed":   map[string]any{"terminal": true},
		},
		"transitions": []any{
			map[string]any{"from": "pending", "input": InputStart, "to": "in_progress"},
			map[string]any{"from": "in_progress", "input": InputComplete, "to": "completed"},
		},
	}
}
