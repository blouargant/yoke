package claudeformat

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// MCPInput mirrors the VS Code mcp.json input schema for a single user-supplied
// value (promptString or pickString). It is stored in Config.Inputs at install time.
type MCPInput struct {
	ID          string   `yaml:"id"`
	Type        string   `yaml:"type"`        // "promptString" (default) or "pickString"
	Description string   `yaml:"description"`
	Password    bool     `yaml:"password"`
	Options     []string `yaml:"options"` // for pickString
	Default     string   `yaml:"default"`
}

// MCPDef is the normalized representation produced after parsing an mcp.md file.
type MCPDef struct {
	Name        string
	Description string
	Command     string
	Args        []string
	Env         map[string]string
	Headers     map[string]string // HTTP headers, e.g. {"Authorization": "Bearer ${input:id}"}
	Type        string            // "stdio" or "http"
	URL         string
	Skills      []string
	Inputs      []MCPInput // user-supplied values referenced via "${input:id}" templates
	Body        string     // markdown body — used as README/documentation content
}

// ParseMCPMarkdown parses an mcp.md file (YAML frontmatter + markdown body).
// The frontmatter must contain at least a "name" field.
func ParseMCPMarkdown(content []byte) (*MCPDef, error) {
	const sep = "---"
	s := string(content)

	if !strings.HasPrefix(strings.TrimSpace(s), sep) {
		return nil, fmt.Errorf("not an mcp.md: missing frontmatter delimiter")
	}

	rest := strings.TrimSpace(s)
	rest = strings.TrimPrefix(rest, sep)
	end := strings.Index(rest, "\n---")
	if end < 0 {
		if strings.HasSuffix(strings.TrimSpace(rest), sep) {
			end = strings.LastIndex(rest, sep)
		}
	}

	var yamlBody, body string
	if end >= 0 {
		yamlBody = rest[:end]
		after := rest[end:]
		after = strings.TrimPrefix(after, "\n---")
		after = strings.TrimPrefix(after, "---")
		body = strings.TrimLeft(after, "\n")
	} else {
		yamlBody = rest
	}

	var fm struct {
		Name        string            `yaml:"name"`
		Description string            `yaml:"description"`
		Command     string            `yaml:"command"`
		Args        []string          `yaml:"args"`
		Env         map[string]string `yaml:"env"`
		Headers     map[string]string `yaml:"headers"`
		Type        string            `yaml:"type"`
		URL         string            `yaml:"url"`
		Skills      stringSlice       `yaml:"skills"`
		Inputs      []MCPInput        `yaml:"inputs"`
	}
	if err := yaml.Unmarshal([]byte(yamlBody), &fm); err != nil {
		return nil, fmt.Errorf("parse mcp.md frontmatter: %w", err)
	}
	if fm.Name == "" {
		return nil, fmt.Errorf("mcp.md missing required 'name' field")
	}

	return &MCPDef{
		Name:        fm.Name,
		Description: fm.Description,
		Command:     fm.Command,
		Args:        fm.Args,
		Env:         fm.Env,
		Headers:     fm.Headers,
		Type:        fm.Type,
		URL:         fm.URL,
		Skills:      fm.Skills,
		Inputs:      fm.Inputs,
		Body:        strings.TrimRight(body, "\n"),
	}, nil
}
