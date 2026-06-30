package settings

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// validLocales is the fixed set of UI locales (mirrors web/i18n).
var validLocales = map[string]bool{"en": true, "fr": true, "es": true, "de": true}

// isDefaultAlias reports whether a value names "revert to the default" rather
// than a concrete setting. An empty string is handled separately by callers.
func isDefaultAlias(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "default", "none", "clear", "reset", "unset":
		return true
	}
	return false
}

// writeSections are the section names update_config / remove_config / get_settings
// understand, with a one-line description for the no-arg get_settings listing.
var sectionCatalogue = []struct{ Name, Desc string }{
	{"preferences", "Web UI preferences: theme, locale, notifications."},
	{"agents", "Enabled agents and their definitions (model, tools, skills, flags)."},
	{"squads", "Squad composition (leader + members), stored in agents.json."},
	{"models", "Model catalogue + providers (credentials redacted) and embed/eval roles."},
	{"permissions", "Tool permission rules (allow/ask/deny) + defaultMode. (sensitive)"},
	{"mcp", "MCP server definitions (mcp_config.json)."},
	{"a2a", "Remote A2A agent endpoints (a2a_config.json)."},
	{"hooks", "Lifecycle hooks (hooks.json). (sensitive)"},
	{"server", "Server settings from server.yaml (read-only via chat)."},
}

// normalizeSection maps user-facing aliases to a canonical section name.
func normalizeSection(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "agent", "agents":
		return "agents"
	case "squad", "squads":
		return "squads"
	case "model", "models":
		return "models"
	case "permission", "permissions", "perms":
		return "permissions"
	case "mcp", "mcp_config", "mcp_servers":
		return "mcp"
	case "a2a", "a2a_config":
		return "a2a"
	case "hook", "hooks":
		return "hooks"
	case "pref", "prefs", "preference", "preferences":
		return "preferences"
	case "server", "server.yaml":
		return "server"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// configEditSection maps a canonical section to the configedit section name for
// sections backed by their own JSON file. The bool is false for sections that
// are not edited through configedit.ReadSection/WriteSection (agents/squads live
// in agents.json; preferences/server are special-cased).
func configEditSection(section string) (string, bool) {
	switch section {
	case "models":
		return "models", true
	case "permissions":
		return "permissions", true
	case "mcp":
		return "mcp", true
	case "a2a":
		return "a2a", true
	case "hooks":
		return "hooks", true
	}
	return "", false
}

// redactKeys are object keys whose string values are masked in get_settings
// output. base_url is intentionally NOT masked — it is rarely secret and useful
// for guidance.
var redactKeys = map[string]bool{
	"api_key": true, "apikey": true, "password": true,
	"secret": true, "token": true, "access_token": true, "authorization": true,
}

// redact walks a freshly-parsed JSON tree and masks the string values of
// credential-bearing keys in place (the tree is a per-call parse, never the
// on-disk data). A non-empty value becomes "***set***"; an empty one is left so
// the user can see the field is unset.
func redact(node any) any {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if redactKeys[strings.ToLower(k)] {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					n[k] = "***set***"
					continue
				}
			}
			n[k] = redact(v)
		}
		return n
	case []any:
		for i, v := range n {
			n[i] = redact(v)
		}
		return n
	default:
		return node
	}
}

// credentialChangeKeys trigger the sensitive-change confirmation when a write
// touches one of them (by field name or JSON-pointer segment). base_url is
// included because pointing a provider at a new endpoint is a security-relevant
// change.
var credentialChangeKeys = map[string]bool{
	"api_key": true, "apikey": true, "base_url": true, "password": true,
	"secret": true, "token": true, "access_token": true, "authorization": true,
	"headers": true,
}

// pointerTouchesCredential reports whether any segment of a path looks like a
// credential field.
func pointerTouchesCredential(path string) bool {
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '.' }) {
		if credentialChangeKeys[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// valueTouchesCredential reports whether a JSON value (object/array) contains a
// credential-bearing key with a non-empty value.
func valueTouchesCredential(node any) bool {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if credentialChangeKeys[strings.ToLower(k)] {
				if s, ok := v.(string); !ok || strings.TrimSpace(s) != "" {
					return true
				}
			}
			if valueTouchesCredential(v) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if valueTouchesCredential(v) {
				return true
			}
		}
	}
	return false
}

// availableThemes lists the theme ids shipped with the web UI by enumerating
// <webDir>/css/themes/*.css (the filename without extension is the theme id).
// Returns nil when the directory cannot be read — callers then accept any
// non-empty id rather than blocking a valid theme.
func availableThemes() []string {
	webDir := strings.TrimSpace(os.Getenv("OMNIS_WEB_DIR"))
	if webDir == "" {
		webDir = "web"
	}
	entries, err := os.ReadDir(filepath.Join(webDir, "css", "themes"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".css") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".css"))
	}
	sort.Strings(out)
	return out
}
