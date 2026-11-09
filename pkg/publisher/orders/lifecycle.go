package orders

// LifecycleDefinition is the FSM that governs a work order:
//
//	Open --start--> InProgress --complete[resolution]--> Completed
//	Open|InProgress --cancel--> Cancelled
//
// "complete" is guarded: the payload must carry a non-empty resolution, so a
// rejected transition has a guard diagnostic worth surfacing verbatim.
func LifecycleDefinition() Definition {
	return Definition{
		"name":        DefinitionName,
		"description": "Lifecycle of a work order from opening to completion or cancellation",
		"initial":     "Open",
		// xolu requires an explicit level (XOLU-FSM006 otherwise). Each
		// (state, input) pair has exactly one transition, so "strict" fits.
		"determinism": "strict",
		"states": map[string]any{
			"Open":       map[string]any{"terminal": false},
			"InProgress": map[string]any{"terminal": false},
			"Completed":  map[string]any{"terminal": true},
			"Cancelled":  map[string]any{"terminal": true},
		},
		"output_alphabet": []any{"order_completed"},
		"transitions": []any{
			map[string]any{"from": "Open", "input": "start", "to": "InProgress"},
			map[string]any{
				"from": "InProgress", "input": "complete", "to": "Completed",
				"guard":  "payload.resolution IS NOT NULL AND payload.resolution != ''",
				"output": "order_completed",
			},
			map[string]any{"from": "Open", "input": "cancel", "to": "Cancelled"},
			map[string]any{"from": "InProgress", "input": "cancel", "to": "Cancelled"},
		},
	}
}

// terminalStates mirrors LifecycleDefinition so the service can tell whether an
// order is closed even when xolu's machine document omits a terminal flag.
var terminalStates = map[string]bool{"Completed": true, "Cancelled": true}
