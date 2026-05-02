package skills

import (
	"embed"
	"io/fs"

	"google.golang.org/adk/tool/skilltoolset/skill"
)

// builtinFS holds the mandatory skills compiled into the binary.
// These skills (review, agent-builder) cannot be modified at runtime.
//
//go:embed data
var builtinFS embed.FS

// builtinSource returns a skill.Source backed by the embedded data directory.
func builtinSource() skill.Source {
	sub, err := fs.Sub(builtinFS, "data")
	if err != nil {
		// fs.Sub on a valid embed.FS subdirectory never fails.
		panic("skills: failed to open embedded data: " + err.Error())
	}
	return skill.NewFileSystemSource(sub)
}
