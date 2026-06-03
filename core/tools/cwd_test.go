package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAgainst(t *testing.T) {
	cases := []struct{ cwd, path, want string }{
		{"/work", "foo.go", "/work/foo.go"},
		{"/work", "sub/foo.go", "/work/sub/foo.go"},
		{"/work", "/abs/foo.go", "/abs/foo.go"}, // absolute unchanged
		{"", "foo.go", "foo.go"},                // no cwd → unchanged
		{"/work", "", ""},                       // empty path → unchanged
	}
	for _, c := range cases {
		if got := resolveAgainst(c.cwd, c.path); got != c.want {
			t.Errorf("resolveAgainst(%q,%q)=%q want %q", c.cwd, c.path, got, c.want)
		}
	}
}

func TestWithCwdContext(t *testing.T) {
	if got := cwdFromContext(context.Background()); got != "" {
		t.Errorf("empty context cwd=%q want \"\"", got)
	}
	ctx := WithCwd(context.Background(), "/work")
	if got := cwdFromContext(ctx); got != "/work" {
		t.Errorf("cwdFromContext=%q want /work", got)
	}
	// Empty cwd is a no-op (does not wrap).
	if got := cwdFromContext(WithCwd(context.Background(), "")); got != "" {
		t.Errorf("WithCwd(\"\") cwd=%q want \"\"", got)
	}
}

func TestRunBashHonorsCwd(t *testing.T) {
	dir := t.TempDir()
	out, err := RunBash(context.Background(), BashIn{Command: "pwd", Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	// macOS /tmp symlinks to /private/tmp; compare resolved paths.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(strings.TrimSpace(out))
	if gotResolved != wantResolved {
		t.Errorf("pwd in cwd=%q gave %q (resolved %q want %q)", dir, out, gotResolved, wantResolved)
	}
}

func TestRunGrepHonorsCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := RunGrep(context.Background(), GrepIn{Pattern: "needle", Path: ".", Recursive: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "needle") {
		t.Errorf("grep in cwd missed the match: %q", out)
	}
}

func TestRunGlobHonorsCwdRelativeOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := RunGlob(context.Background(), GlobIn{Pattern: "*.go", Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	// Matches are reported relative to cwd, not as absolute paths.
	if strings.TrimSpace(out) != "x.go" {
		t.Errorf("glob output=%q want \"x.go\" (relative to cwd)", out)
	}
}
