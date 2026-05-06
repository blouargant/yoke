package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestMarkdownRendererKeepsHeadingsAndTLDRReadable(t *testing.T) {
	renderer := &markdownRenderer{}
	input := "### Check\n\nTL;DR: the GPU manager is failing, not the workload pod."

	plain := stripTerminalControlSequences(renderer.render(input, 60))

	if !strings.Contains(plain, "Check") {
		t.Fatalf("rendered markdown missing heading text: %q", plain)
	}
	if !strings.Contains(plain, "TL;DR") {
		t.Fatalf("rendered markdown split or removed TL;DR marker: %q", plain)
	}
	if !strings.Contains(plain, "### Check") {
		t.Fatalf("rendered markdown split heading marker from text: %q", plain)
	}
}

func TestMarkdownRendererRendersTableWithoutRawSeparator(t *testing.T) {
	renderer := &markdownRenderer{}
	input := strings.Join([]string{
		"| Component | Status | Detail |",
		"| --- | --- | --- |",
		"| Pod blo | Running | 0 restarts, scheduled on node4 |",
		"| gpu-manager-thnt7 | CrashLoopBackOff | Only GPU-manager DaemonSet pod - critical failure |",
	}, "\n")

	plain := stripTerminalControlSequences(renderer.render(input, 80))

	for _, want := range []string{"Component", "Status", "Detail", "gpu-manager-thnt7", "CrashLoopBackOff"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered table missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "| --- |") || strings.Contains(plain, "|---|") {
		t.Fatalf("rendered table still contains raw markdown separator: %q", plain)
	}
}

func TestMarkdownRendererStripsTerminalControlSequences(t *testing.T) {
	renderer := &markdownRenderer{}
	input := "\x1b[31m### Alert\x1b[0m\n\nBody"

	plain := stripTerminalControlSequences(renderer.render(input, 60))

	if !strings.Contains(plain, "Alert") {
		t.Fatalf("rendered markdown missing sanitized heading text: %q", plain)
	}
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("rendered markdown contains escape sequence after stripping: %q", plain)
	}
}

func TestMarkdownRendererFallsBackToDefaultWidth(t *testing.T) {
	renderer := &markdownRenderer{}

	_ = renderer.render("hello", 0)

	if renderer.width != 80 {
		t.Fatalf("renderer width = %d, want default width 80", renderer.width)
	}
}

func TestNewChatTextViewDisablesWordWrap(t *testing.T) {
	chat := newChatTextView(nil)
	value := reflect.ValueOf(chat).Elem()

	if !value.FieldByName("wrap").Bool() {
		t.Fatalf("chat wrap = false, want true")
	}
	if value.FieldByName("wordWrap").Bool() {
		t.Fatalf("chat wordWrap = true, want false so Glamour owns word wrapping")
	}
}
