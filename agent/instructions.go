package agent

import (
	"embed"
	"strings"
)

//go:embed instructions/*.md
var defaultInstructionsFS embed.FS

// readEmbeddedInstruction returns the trimmed contents of
// instructions/<name>.md, or "" if the file does not exist.
func readEmbeddedInstruction(name string) string {
	b, err := defaultInstructionsFS.ReadFile("instructions/" + name + ".md")
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}
