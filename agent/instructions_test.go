package agent

import "testing"

func TestDefaultAgentInstructionEmbedded(t *testing.T) {
	for _, name := range []string{"leader", "investigator", "summariser"} {
		got := defaultAgentInstruction(name)
		if got == "" {
			t.Errorf("defaultAgentInstruction(%q) returned empty", name)
		}
	}
	// Unknown name must fall back to the default.
	if defaultAgentInstruction("does-not-exist") == "" {
		t.Error("defaultAgentInstruction fallback returned empty")
	}
}
