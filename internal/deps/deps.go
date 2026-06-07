// Package deps models the runtime tool dependencies a skill or MCP server can
// declare — a binary that must be on PATH plus how to install it — and provides
// an application-level gate (Ensure) that checks availability, asks the user for
// consent, installs, and rechecks. The point is to enforce dependency
// installation in code rather than relying on the model to follow a prompt: a
// skill or MCP server declares `requires:` and the host guarantees the check
// fires.
package deps

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Requirement is one runtime dependency: a binary that must resolve on PATH,
// plus the command that installs it. Label is an optional human-friendly name.
type Requirement struct {
	Command string  `json:"command" yaml:"command"`
	Install Install `json:"install,omitempty" yaml:"install,omitempty"`
	Label   string  `json:"label,omitempty" yaml:"label,omitempty"`
}

// Name returns the human-friendly label, falling back to the command.
func (r Requirement) Name() string {
	if s := strings.TrimSpace(r.Label); s != "" {
		return s
	}
	return r.Command
}

// Install is the install command for a Requirement. It accepts, in both YAML
// and JSON, either a single string ("pip install liteparse") or a per-OS map
// keyed by GOOS with an optional "default" ({default: …, linux: …, darwin: …}).
type Install struct {
	Default string
	PerOS   map[string]string
}

// Command resolves the install command for the current OS, falling back to the
// default. Returns "" when nothing is declared.
func (i Install) Command() string {
	if i.PerOS != nil {
		if c, ok := i.PerOS[runtime.GOOS]; ok {
			if c = strings.TrimSpace(c); c != "" {
				return c
			}
		}
	}
	return strings.TrimSpace(i.Default)
}

// Empty reports whether no install command resolves for the current OS.
func (i Install) Empty() bool { return i.Command() == "" }

func (i *Install) absorbMap(m map[string]string) {
	i.PerOS = map[string]string{}
	for k, v := range m {
		if strings.EqualFold(strings.TrimSpace(k), "default") {
			i.Default = strings.TrimSpace(v)
			continue
		}
		i.PerOS[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
}

// UnmarshalYAML accepts a scalar string or a per-OS mapping.
func (i *Install) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		i.Default = strings.TrimSpace(value.Value)
		return nil
	}
	var m map[string]string
	if err := value.Decode(&m); err != nil {
		return err
	}
	i.absorbMap(m)
	return nil
}

// UnmarshalJSON accepts a scalar string or a per-OS object.
func (i *Install) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		i.Default = strings.TrimSpace(s)
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	i.absorbMap(m)
	return nil
}

// MarshalJSON emits a bare string when only a default is set (keeping config
// files tidy), otherwise a per-OS object with the default folded back in.
func (i Install) MarshalJSON() ([]byte, error) {
	if len(i.PerOS) == 0 {
		return json.Marshal(i.Default)
	}
	m := make(map[string]string, len(i.PerOS)+1)
	for k, v := range i.PerOS {
		m[k] = v
	}
	if i.Default != "" {
		m["default"] = i.Default
	}
	return json.Marshal(m)
}

// Present reports whether cmd resolves on PATH. An empty command is treated as
// present (nothing to check).
func Present(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return true
	}
	_, err := exec.LookPath(cmd)
	return err == nil
}

// Missing returns the requirements whose command is not currently on PATH.
func Missing(reqs []Requirement) []Requirement {
	var out []Requirement
	for _, r := range reqs {
		if !Present(r.Command) {
			out = append(out, r)
		}
	}
	return out
}

// Confirmer asks the user, in the active session, to approve installing a
// requirement. It returns true to proceed with the install.
type Confirmer interface {
	ConfirmInstall(ctx context.Context, sessionID string, req Requirement) (bool, error)
}

// Installer runs an install command on the host and returns its output. It
// should enforce the same safety floor as a normal Bash tool call.
type Installer func(ctx context.Context, command string) (string, error)

// Outcome reports what happened for one requirement that was missing at the
// start of Ensure. Requirements already present are not reported.
type Outcome struct {
	Requirement Requirement
	Installed   bool   // was missing, now present because we installed it
	Available   bool   // present at the end of Ensure
	Reason      string // when unavailable: why (declined / no installer / install failed / still missing)
}

// Ensure checks each requirement and, for every one that is missing, asks the
// user for consent (via c) and installs it (via install), then rechecks. It
// returns one Outcome per initially-missing requirement; an empty result means
// every dependency was already satisfied. Requirements that remain unavailable
// carry Available=false and a human-readable Reason. A nil Confirmer or
// Installer degrades gracefully (the requirement is reported unavailable).
func Ensure(ctx context.Context, sessionID string, reqs []Requirement, c Confirmer, install Installer) []Outcome {
	var out []Outcome
	for _, r := range reqs {
		if strings.TrimSpace(r.Command) == "" || Present(r.Command) {
			continue
		}
		o := Outcome{Requirement: r}
		switch {
		case r.Install.Empty():
			o.Reason = "no install command is declared for " + r.Command
		case c == nil:
			o.Reason = "cannot prompt for installation in this context"
		default:
			ok, err := c.ConfirmInstall(ctx, sessionID, r)
			switch {
			case err != nil:
				o.Reason = "install prompt failed: " + err.Error()
			case !ok:
				o.Reason = "user declined to install " + r.Name()
			case install == nil:
				o.Reason = "no installer is available in this context"
			default:
				_, ierr := install(ctx, r.Install.Command())
				switch {
				case Present(r.Command):
					o.Installed = true
					o.Available = true
				case ierr != nil:
					o.Reason = "install command failed: " + ierr.Error()
				default:
					o.Reason = r.Command + " is still not on PATH after install (it may need a new shell or a PATH update)"
				}
			}
		}
		out = append(out, o)
	}
	return out
}
