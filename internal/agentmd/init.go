package agentmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// initPrompt is the shared "/init" instruction sent to the leader as a normal
// user turn on every surface (web UI, TUI, CLI). It asks the agent to inspect
// the repository and write a starter AGENT.md.
const initPrompt = `Analyze this project and create an ` + FileName + ` file at its root that documents how an AI agent should work here. The project may be code, but it may equally be documentation, data, research, configuration, or prose — adapt to whatever you actually find rather than assuming a software codebase.

First explore with your file-system tools: read the README or equivalent overview, any manifest/build/config files, and a representative sample of the contents. Then write ` + FileName + ` (if it already exists, read it first and preserve anything still accurate). Include only the sections that apply; common ones are:

- A one-line description of what the project is and its purpose.
- **Commands / workflows**: how to build, test, run, lint, or otherwise operate on it — only if such commands exist.
- **Structure**: the key files, directories, or components and their roles.
- **Conventions**: naming, formatting, and patterns a contributor must follow.
- **Gotchas**: non-obvious rules, precedence, or pitfalls — for each, name the trap and the symptom of getting it wrong.

The document is only useful if it is correct, so:

- Verify every exact token (a command, path, filename, key, identifier) by reading the file that defines or produces it — do not write it from assumption. If you cannot confirm a literal, describe its shape ("a per-item JSON file under the logs directory") rather than inventing a precise name.
- When project documentation, READMEs, or skill/notes files state a literal (a path, command, or name), treat them as leads to verify, not as authoritative — confirm against the code or file that actually defines it, since docs drift out of sync with the implementation.
- When you list a set (commands, tools, fields, subdirectories), enumerate the complete set from its source of truth, not a sample — readers treat an omission as "does not exist".
- Do not hard-code values that drift (version numbers, counts, dates, ports, model or dependency names). Name the file that holds the current value instead, or use a neutral placeholder.
- State the general rule first; mark special cases as exceptions rather than presenting an exception as the rule.
- Favor what an agent cannot cheaply rediscover by looking around — gotchas, precedence, rationale, conventions — over exhaustive file-by-file inventories, which go stale fastest. Aim for a document a fresh agent can absorb in one read.

Keep it factual and derived from what is actually present — do not invent. When done, briefly summarize what you wrote.`

// InitPrompt returns the shared "/init" bootstrap instruction.
func InitPrompt() string { return initPrompt }

// AppendMemory appends a one-line memory (the "#" shortcut) to the project
// AGENT.md resolved from cwd, creating the file with a heading when missing.
// The target is the repo root's AGENT.md (or cwd's when not in a repo). It
// returns the absolute path written. A leading "#" and surrounding whitespace
// on line are stripped; an empty line after trimming is an error.
func AppendMemory(cwd, line string) (string, error) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	if line == "" {
		return "", fmt.Errorf("empty memory")
	}
	if strings.TrimSpace(cwd) == "" {
		c, err := os.Getwd()
		if err != nil {
			return "", err
		}
		cwd = c
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	target := filepath.Join(repoRoot(abs), FileName)

	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var b strings.Builder
	if len(existing) == 0 {
		b.WriteString("# " + FileName + "\n\nProject memory for AI agents.\n\n## Notes\n")
	} else {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteString("\n")
		}
		// Ensure a Notes section exists; append one when absent.
		if !strings.Contains(string(existing), "## Notes") {
			b.WriteString("\n## Notes\n")
		}
	}
	b.WriteString("- " + line + "\n")
	if err := os.WriteFile(target, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return target, nil
}
