// Package configedit holds the layer-aware configuration read/write logic shared
// by the HTTP server (the web-UI editor) and the in-process settings tools
// mounted on the Helper agent. It is deliberately low-level: it depends only on
// internal/paths + the standard library, so both the server (package main) and
// the agent process (which cannot import the server) can use one implementation
// of "where does this write land".
package configedit

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/blouargant/omnis/internal/paths"
)

// SourceLayer returns "local" or "user" based on where the file currently lives.
// Files that resolve to /etc/omnis (or are missing entirely) fork into "user" —
// omnis never writes back into the system layer.
func SourceLayer(readPath string) string {
	if readPath == "" {
		return "user"
	}
	switch paths.Layer(readPath) {
	case "local":
		return "local"
	default:
		return "user"
	}
}

// AgentsConfigLayer decides where the top-level agents.json should be written,
// given the file's current source path and its parsed body. Promotes to "local"
// when any referenced agent or skill lives only in a local-layer directory;
// otherwise preserves the source layer (forking system → user).
func AgentsConfigLayer(readPath string, body []byte) string {
	base := SourceLayer(readPath)
	if base == "local" {
		return "local" // already local — stay local regardless.
	}
	if len(body) == 0 {
		return base
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return base
	}
	if agentsListHasLocalOnlyReference(parsed) {
		return "local"
	}
	return base
}

// AgentTargetLayer decides where a per-agent agent.json should be written.
// Considers:
//   - the layer of any skills it now references (promotes to local when any
//     skill is local-only),
//   - the agent's current source layer (if any).
//
// Defaults to "user" when no information is available.
func AgentTargetLayer(name string, skills []string) string {
	for _, skill := range skills {
		if isSkillLocalOnly(skill) {
			return "local"
		}
	}
	switch paths.AgentSourceLayer(name) {
	case "local":
		return "local"
	case "system":
		// /etc/omnis is read-only; fork into user when editing.
		return "user"
	case "user":
		return "user"
	}
	return "user"
}

// agentsListHasLocalOnlyReference scans the parsed `agents` array and the
// `squads.members` list, looking for any agent name that resides only in a
// local-layer registry directory or whose definition references skills that are
// local-only.
func agentsListHasLocalOnlyReference(parsed map[string]any) bool {
	if parsed == nil {
		return false
	}
	for _, name := range collectAgentNames(parsed) {
		if isAgentLocalOnly(name) {
			return true
		}
		for _, skill := range loadAgentSkills(name) {
			if isSkillLocalOnly(skill) {
				return true
			}
		}
	}
	return false
}

// collectAgentNames returns the union of names from `agents` (top-level list of
// agent names or objects) and `squads[*].leader|members`.
func collectAgentNames(parsed map[string]any) []string {
	seen := map[string]struct{}{}
	add := func(n string) {
		if n != "" {
			seen[n] = struct{}{}
		}
	}
	if list, ok := parsed["agents"].([]any); ok {
		for _, item := range list {
			switch v := item.(type) {
			case string:
				add(v)
			case map[string]any:
				if s, _ := v["name"].(string); s != "" {
					add(s)
				}
			}
		}
	}
	if squads, ok := parsed["squads"].([]any); ok {
		for _, item := range squads {
			sq, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := sq["leader"].(string); s != "" {
				add(s)
			}
			if members, ok := sq["members"].([]any); ok {
				for _, m := range members {
					if s, ok := m.(string); ok {
						add(s)
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	return out
}

// loadAgentSkills returns the skills list declared in the highest-precedence
// agent.json for the given name, or nil when the agent has no definition or no
// skills field.
func loadAgentSkills(name string) []string {
	for _, dir := range paths.AgentsRegistrySearchDirs() {
		p := filepath.Join(dir, name, "agent.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var def struct {
			Skills []string `json:"skills"`
		}
		if err := json.Unmarshal(data, &def); err != nil {
			return nil
		}
		return def.Skills
	}
	return nil
}

// isAgentLocalOnly reports whether the named agent's agent.json exists in at
// least one local-layer directory and in no other layer. A brand-new agent (no
// agent.json anywhere) returns false: there is no existing layer to promote to.
func isAgentLocalOnly(name string) bool {
	var foundLocal, foundOther bool
	for _, dir := range paths.AgentsRegistrySearchDirs() {
		p := filepath.Join(dir, name, "agent.json")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if paths.Layer(dir) == "local" {
			foundLocal = true
		} else {
			foundOther = true
		}
	}
	return foundLocal && !foundOther
}

// isSkillLocalOnly reports whether the named skill's SKILL.md exists in at least
// one local-layer directory and in no other layer.
func isSkillLocalOnly(name string) bool {
	var foundLocal, foundOther bool
	for _, dir := range paths.SkillsAllSearchDirs() {
		p := filepath.Join(dir, name, "SKILL.md")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if paths.Layer(dir) == "local" {
			foundLocal = true
		} else {
			foundOther = true
		}
	}
	return foundLocal && !foundOther
}
