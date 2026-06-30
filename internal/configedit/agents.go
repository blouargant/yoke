package configedit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blouargant/omnis/internal/paths"
)

// ReadAgentsConfig reads and parses the top-level agents.json (the `agents`
// names list + `squads`). Returns nil parsed when absent. Used by the settings
// tools to enumerate which agents are wired and to read squad composition.
func ReadAgentsConfig() (parsed map[string]any, readPath, layer string, err error) {
	p, _ := ReadPath("agent")
	readPath = p
	layer = paths.Layer(p)
	data, rerr := os.ReadFile(p)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return map[string]any{}, readPath, layer, nil
		}
		return nil, readPath, layer, rerr
	}
	if len(data) == 0 {
		return map[string]any{}, readPath, layer, nil
	}
	if uerr := json.Unmarshal(data, &parsed); uerr != nil {
		return nil, readPath, layer, fmt.Errorf("agents.json is not valid JSON: %w", uerr)
	}
	return parsed, readPath, layer, nil
}

// ReadAgentEntry reads the highest-precedence registry/agents/<name>/agent.json
// for the named agent. It returns the parsed entry, the layer the file lives in,
// and its path. Returns os.ErrNotExist when no definition is found in any layer.
func ReadAgentEntry(name string) (entry map[string]any, layer, path string, err error) {
	for _, dir := range paths.AgentsRegistrySearchDirs() {
		p := filepath.Join(dir, name, "agent.json")
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		if uerr := json.Unmarshal(data, &entry); uerr != nil {
			return nil, "", p, fmt.Errorf("%s is not valid JSON: %w", p, uerr)
		}
		return entry, paths.Layer(dir), p, nil
	}
	return nil, "", "", os.ErrNotExist
}

// AgentSkills extracts the declared skills list from a parsed agent entry.
func AgentSkills(entry map[string]any) []string {
	var out []string
	if raw, ok := entry["skills"].([]any); ok {
		for _, s := range raw {
			if sn, ok := s.(string); ok && sn != "" {
				out = append(out, sn)
			}
		}
	}
	return out
}

// WriteAgentEntry writes a per-agent agent.json into the layer-appropriate
// registry directory (AgentTargetLayer, considering the agent's source layer and
// whether its declared skills are local-only). An "instruction" string key, if
// present, is peeled off and written to instruction.md alongside (mirroring the
// web-UI editor's fan-out). Returns the agent.json path and the resolved layer.
func WriteAgentEntry(name string, entry map[string]any) (path, layer string, err error) {
	if name == "" {
		return "", "", fmt.Errorf("agent name is empty")
	}
	// Peel instruction off so it never lands inside agent.json.
	var instruction string
	if instr, ok := entry["instruction"].(string); ok {
		instruction = instr
	}
	clean := make(map[string]any, len(entry))
	for k, v := range entry {
		if k == "instruction" {
			continue
		}
		clean[k] = v
	}

	layer = AgentTargetLayer(name, AgentSkills(clean))
	dir := filepath.Join(paths.AgentsRegistryWriteDirForLayer(layer), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir registry agent: %w", err)
	}

	out, merr := json.MarshalIndent(clean, "", "  ")
	if merr != nil {
		return "", "", fmt.Errorf("marshal agent: %w", merr)
	}
	out = append(out, '\n')
	path = filepath.Join(dir, "agent.json")
	if werr := AtomicWriteFile(path, out); werr != nil {
		return "", "", fmt.Errorf("write agent: %w", werr)
	}
	if instruction != "" {
		if werr := AtomicWriteFile(filepath.Join(dir, "instruction.md"), []byte(instruction)); werr != nil {
			return "", "", fmt.Errorf("write instruction: %w", werr)
		}
	}
	return path, layer, nil
}
