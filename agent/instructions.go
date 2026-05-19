package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/blouargant/yoke/internal/paths"
)

// ReadAgentInstruction returns the trimmed contents of the agent's instruction file
// from registry/agents/<name>/instruction.md, or falls back to registry/agents/default.md,
// or "" if neither exist.
func ReadAgentInstruction(name string) string {
	dirs := paths.AgentsRegistrySearchDirs()

	// Try agent-specific instruction across all registry layers.
	for _, dir := range dirs {
		if b, err := os.ReadFile(filepath.Join(dir, name, "instruction.md")); err == nil {
			return strings.TrimRight(string(b), "\n")
		}
	}

	// Fall back to default instruction across all registry layers.
	for _, dir := range dirs {
		if b, err := os.ReadFile(filepath.Join(dir, "default.md")); err == nil {
			return strings.TrimRight(string(b), "\n")
		}
	}

	return ""
}
