package main

import "github.com/blouargant/omnis/internal/configedit"

// The layer-resolution logic lives in internal/configedit so the in-process
// settings tools (mounted on the Helper agent, which cannot import the server)
// share one implementation of "where does this write land". These thin wrappers
// keep the server's existing call sites unchanged.

// resolveSourceLayer returns "local" or "user" based on where the file currently
// lives. Files that resolve to /etc/omnis (or are missing) fork into "user".
func resolveSourceLayer(readPath string) string {
	return configedit.SourceLayer(readPath)
}

// resolveAgentsConfigLayer decides where the top-level agents.json should be
// written, promoting to "local" when the parsed body references any local-only
// agent or skill; otherwise preserving the source layer (system → user fork).
func resolveAgentsConfigLayer(readPath string, body []byte) string {
	return configedit.AgentsConfigLayer(readPath, body)
}

// agentTargetLayer decides where a per-agent agent.json should be written,
// considering the agent's source layer and whether any referenced skill is
// local-only. Defaults to "user".
func agentTargetLayer(name string, skills []string) string {
	return configedit.AgentTargetLayer(name, skills)
}
