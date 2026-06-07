package deps

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInstallParseScalarAndMap(t *testing.T) {
	t.Parallel()

	// YAML scalar form.
	var y struct {
		Requires []Requirement `yaml:"requires"`
	}
	src := "requires:\n" +
		"  - command: lit\n" +
		"    label: LiteParse\n" +
		"    install: pip install liteparse\n"
	if err := yaml.Unmarshal([]byte(src), &y); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if len(y.Requires) != 1 || y.Requires[0].Command != "lit" {
		t.Fatalf("unexpected requires: %+v", y.Requires)
	}
	if got := y.Requires[0].Install.Command(); got != "pip install liteparse" {
		t.Errorf("scalar install = %q", got)
	}
	if y.Requires[0].Name() != "LiteParse" {
		t.Errorf("label fallback wrong: %q", y.Requires[0].Name())
	}

	// JSON per-OS map form resolves for the current GOOS, falling back to default.
	raw := `{"command":"foo","install":{"default":"def-cmd","` + runtime.GOOS + `":"os-cmd"}}`
	var r Requirement
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got := r.Install.Command(); got != "os-cmd" {
		t.Errorf("per-OS install = %q, want os-cmd", got)
	}

	// Map without the current GOOS falls back to default.
	var r2 Requirement
	if err := json.Unmarshal([]byte(`{"command":"foo","install":{"default":"def-cmd","plan9":"x"}}`), &r2); err != nil {
		t.Fatalf("json2: %v", err)
	}
	if got := r2.Install.Command(); got != "def-cmd" {
		t.Errorf("default fallback = %q", got)
	}

	// YAML per-OS map (the pdf skill's poppler shape) resolves for the current OS.
	var y2 struct {
		Requires []Requirement `yaml:"requires"`
	}
	mapSrc := "requires:\n" +
		"  - command: pdftotext\n" +
		"    install:\n" +
		"      darwin: brew install poppler\n" +
		"      linux: apt-get install -y poppler-utils\n"
	if err := yaml.Unmarshal([]byte(mapSrc), &y2); err != nil {
		t.Fatalf("yaml per-OS: %v", err)
	}
	want := map[string]string{"darwin": "brew install poppler", "linux": "apt-get install -y poppler-utils"}[runtime.GOOS]
	if got := y2.Requires[0].Install.Command(); got != want {
		t.Errorf("yaml per-OS install for %s = %q, want %q", runtime.GOOS, got, want)
	}
}

func TestPresentMissing(t *testing.T) {
	t.Parallel()
	// "go" is on PATH in the test environment; a random name is not.
	reqs := []Requirement{
		{Command: "go"},
		{Command: "definitely-not-a-real-binary-zzz"},
		{Command: ""}, // empty → treated present, skipped by Missing
	}
	miss := Missing(reqs)
	if len(miss) != 1 || miss[0].Command != "definitely-not-a-real-binary-zzz" {
		t.Fatalf("Missing = %+v", miss)
	}
}

type fakeConfirmer struct {
	yes    bool
	asked  int
	lastID string
}

func (f *fakeConfirmer) ConfirmInstall(_ context.Context, sessionID string, _ Requirement) (bool, error) {
	f.asked++
	f.lastID = sessionID
	return f.yes, nil
}

func TestEnsureDeclineReportsUnavailable(t *testing.T) {
	t.Parallel()
	missing := "definitely-not-a-real-binary-zzz"
	reqs := []Requirement{{Command: missing, Install: Install{Default: "echo noop"}}}
	conf := &fakeConfirmer{yes: false}
	installed := false
	run := func(context.Context, string) (string, error) { installed = true; return "", nil }

	out := Ensure(context.Background(), "sess-1", reqs, conf, run)
	if len(out) != 1 || out[0].Available {
		t.Fatalf("declined dep should be unavailable: %+v", out)
	}
	if installed {
		t.Error("installer must not run when the user declines")
	}
	if conf.asked != 1 || conf.lastID != "sess-1" {
		t.Errorf("confirmer not consulted with session id: asked=%d id=%q", conf.asked, conf.lastID)
	}
}

func TestEnsureNoInstallCommand(t *testing.T) {
	t.Parallel()
	reqs := []Requirement{{Command: "definitely-not-a-real-binary-zzz"}} // no install
	conf := &fakeConfirmer{yes: true}
	out := Ensure(context.Background(), "s", reqs, conf, nil)
	if len(out) != 1 || out[0].Available {
		t.Fatalf("dep with no installer should be unavailable: %+v", out)
	}
	if conf.asked != 0 {
		t.Error("should not prompt when there is no install command")
	}
}

func TestEnsureSkipsPresent(t *testing.T) {
	t.Parallel()
	conf := &fakeConfirmer{yes: true}
	out := Ensure(context.Background(), "s", []Requirement{{Command: "go", Install: Install{Default: "x"}}}, conf, nil)
	if len(out) != 0 {
		t.Fatalf("present dep should not be reported: %+v", out)
	}
	if conf.asked != 0 {
		t.Error("present dep should not prompt")
	}
}
