package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// persistApproval appends an always_allow rule for (toolName, input) to
// the given user config file. The rule's CWD field, when non-empty,
// scopes the approval to that project root. An empty cwd persists a
// global allow rule.
//
// The function is idempotent: if an equivalent rule (same pattern, same
// CWD) already exists, the file is left untouched.
func persistApproval(path, toolName, input, cwd string) error {
	if path == "" {
		return fmt.Errorf("user config path not configured")
	}

	pattern, reason := buildApprovalRule(toolName, input, cwd)

	rules := &Rules{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, rules); err != nil {
			return fmt.Errorf("parse existing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	for _, r := range rules.AlwaysAllow {
		if r.Pattern == pattern && r.CWD == cwd {
			return nil // already persisted
		}
	}

	rules.AlwaysAllow = append(rules.AlwaysAllow, Rule{
		Pattern: pattern,
		Reason:  reason,
		CWD:     cwd,
	})

	return writeRulesAtomic(path, rules)
}

// buildApprovalRule constructs an anchored, regex-escaped pattern that
// matches the exact tool call (and the bash command line for the Bash
// tool). Returns the pattern plus a human-readable reason string.
func buildApprovalRule(toolName, input, cwd string) (string, string) {
	args := map[string]any{}
	_ = json.Unmarshal([]byte(input), &args)

	scope := "always"
	if cwd != "" {
		scope = "for this project (" + cwd + ")"
	}

	switch toolName {
	case "Bash":
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			return "^Bash \\{\"command\":\"" + regexp.QuoteMeta(cmd) + "\"",
				"User-approved shell command " + scope
		}
	case "Read", "Write", "Edit", "revert":
		if fp, ok := args["file_path"].(string); ok && fp != "" {
			return "^" + regexp.QuoteMeta(toolName) + " .*\"file_path\":\"" + regexp.QuoteMeta(fp) + "\"",
				"User-approved " + toolName + " on " + fp + " " + scope
		}
	}
	// Fallback: literal probe match. Anchored to toolName + exact input.
	probe := toolName + " " + input
	return "^" + regexp.QuoteMeta(probe) + "$", "User-approved tool call " + scope
}

// writeRulesAtomic marshals rules to path via a temp file + rename, so
// concurrent readers never see a half-written file.
func writeRulesAtomic(path string, rules *Rules) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	data, err := jsonMarshalIndent(rules)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".permissions-*.json")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func jsonMarshalIndent(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(sb.String(), "\n") + "\n"), nil
}
