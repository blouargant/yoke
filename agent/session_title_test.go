package agent

import "testing"

func TestHeuristicTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "   \n\t ", ""},
		{"short", "how does session naming work", "how does session naming work"},
		{"collapses whitespace", "fix   the\n\nlogin   bug", "fix the login bug"},
		{
			"truncates on word boundary",
			"please explain in great detail how the omnis router decides which squad handles a request",
			"please explain in great detail how the omnis router decides…",
		},
		{"drops fenced code", "look at this:\n```go\nfunc main(){}\n```\nwhat's wrong?", "look at this: what's wrong?"},
		{"only code yields empty-ish", "```\njust code\n```", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HeuristicTitle(tc.in)
			if got != tc.want {
				t.Errorf("HeuristicTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if n := len([]rune(got)); n > titleMaxLen {
				t.Errorf("title too long: %d runes > %d (%q)", n, titleMaxLen, got)
			}
		})
	}
}

func TestCleanLLMTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"Claude Code session naming"`, "Claude Code session naming"},
		{"Session naming in Claude Code.", "Session naming in Claude Code"},
		{"  Login bug fix  \n\nextra line", "Login bug fix"},
		{"`code title`", "code title"},
		{"", ""},
		{"Title: How routing works", "Title: How routing works"},
	}
	for _, tc := range cases {
		got := cleanLLMTitle(tc.in)
		if got != tc.want {
			t.Errorf("cleanLLMTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
