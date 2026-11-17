package orders

import "testing"

// xolu rejects definitions without an explicit determinism level (XOLU-FSM006).
// The shape must match what seam-CURE creates, so both share one definition.
func TestLifecycleDefinition_MatchesSeam(t *testing.T) {
	def := LifecycleDefinition()
	if def["name"] != "seam_work_order_lifecycle" || def["initial"] != "draft" || def["determinism"] != "firstmatch" {
		t.Errorf("definition header = %v", def)
	}
	states, _ := def["states"].(map[string]any)
	if len(states) != 3 {
		t.Errorf("states = %v, want draft, in_progress, completed", states)
	}
	if completed, _ := states["completed"].(map[string]any); completed["terminal"] != true {
		t.Errorf("completed must be terminal: %v", states)
	}
	trs, _ := def["transitions"].([]any)
	if len(trs) != 2 {
		t.Fatalf("transitions = %v", trs)
	}
	complete, _ := trs[1].(map[string]any)
	if complete["input"] != "complete" || complete["guard"] != "payload.technician_signature != ''" {
		t.Errorf("complete transition = %v", complete)
	}
	if _, ok := def["output_alphabet"]; ok {
		t.Errorf("Seam's definition declares no output alphabet")
	}
}
