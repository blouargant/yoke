package permissions

import (
	"regexp"
	"strings"
)

// compileBashGlob turns a Claude-style Bash glob specifier into an anchored
// regexp. Semantics (per the Claude Code permission docs):
//
//   - "*" matches any sequence of characters, including spaces, so one
//     wildcard can span multiple arguments.
//   - A trailing " *" (space then star) enforces a word boundary: the prefix
//     must be followed by whitespace or end-of-string. So "ls *" matches
//     "ls -la" and bare "ls" but not "lsof".
//   - A trailing ":*" is equivalent to a trailing " *".
//   - "ls*" (no space) has no boundary and matches both "ls -la" and "lsof".
func compileBashGlob(arg string) (*regexp.Regexp, error) {
	boundary := false
	switch {
	case strings.HasSuffix(arg, ":*"):
		arg = arg[:len(arg)-2]
		boundary = true
	case strings.HasSuffix(arg, " *"):
		arg = arg[:len(arg)-2]
		boundary = true
	}

	var b strings.Builder
	b.WriteString("(?s)^")
	for i, seg := range strings.Split(arg, "*") {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(seg))
	}
	if boundary {
		b.WriteString(`(?:\s.*)?`)
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// bashGlobMatch reports whether a Bash spec matches a single (already
// wrapper-stripped) subcommand string.
func (s *Spec) bashGlobMatch(sub string) bool {
	sub = strings.TrimSpace(sub)
	if s.Bare {
		return true
	}
	if s.glob == nil {
		return false
	}
	return s.glob.MatchString(sub)
}

// compoundSeps are the shell operators Claude Code recognises as command
// separators. A permission rule must match each subcommand independently.
var compoundOps = []string{"&&", "||", "|&", ";", "\n", "|", "&"}

// splitCompound breaks a shell command line into its subcommands, respecting
// single and double quotes so an operator inside a quoted string is not
// treated as a separator. Order is preserved; empty fragments are dropped.
func splitCompound(cmd string) []string {
	var out []string
	var cur strings.Builder
	var quote byte // 0, '\'' or '"'
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			cur.WriteByte(c)
			continue
		}
		matched := ""
		for _, op := range compoundOps {
			if strings.HasPrefix(cmd[i:], op) {
				matched = op
				break
			}
		}
		// A lone '&' that belongs to a file-descriptor redirection
		// (2>&1, >&2, >&-, &>file, &>>file) is NOT a command separator —
		// splitting it would invent a bogus "1"/"2" subcommand that no
		// allow rule covers, forcing an otherwise-allowed command to ask.
		if matched == "&" && isRedirectAmp(cmd, i) {
			matched = ""
		}
		if matched != "" {
			flush()
			i += len(matched) - 1
			continue
		}
		cur.WriteByte(c)
	}
	flush()
	if len(out) == 0 {
		return []string{strings.TrimSpace(cmd)}
	}
	return out
}

// isRedirectAmp reports whether the single '&' at cmd[i] is part of a
// file-descriptor redirection rather than a background/compound operator. It
// is a redirect when it directly follows a '>' (2>&1, >&2, >&-) or directly
// precedes one (&>file, &>>file); intervening spaces/tabs are skipped.
func isRedirectAmp(cmd string, i int) bool {
	j := i - 1
	for j >= 0 && (cmd[j] == ' ' || cmd[j] == '\t') {
		j--
	}
	if j >= 0 && cmd[j] == '>' {
		return true
	}
	k := i + 1
	for k < len(cmd) && (cmd[k] == ' ' || cmd[k] == '\t') {
		k++
	}
	return k < len(cmd) && cmd[k] == '>'
}

// commandWrappers are process wrappers Claude Code strips before matching, so
// a rule like Bash(npm test *) also matches "timeout 30 npm test".
var commandWrappers = map[string]bool{
	"timeout": true, "time": true, "nice": true, "nohup": true, "stdbuf": true,
}

// durationOrNumber loosely matches a wrapper argument like "30", "30s", "1m".
var durationOrNumber = regexp.MustCompile(`^\d+[a-zA-Z]*$`)

// stripWrappers removes recognised process wrappers (and their immediate
// option/duration arguments) from the front of a subcommand, returning the
// inner command. Bare "xargs" (no flags) is also stripped; "xargs -n1 …" is
// left intact (matched as an xargs command, per the docs).
func stripWrappers(sub string) string {
	for {
		fields := strings.Fields(sub)
		if len(fields) == 0 {
			return sub
		}
		head := fields[0]
		if head == "xargs" {
			if len(fields) > 1 && strings.HasPrefix(fields[1], "-") {
				return sub // xargs with flags — not stripped
			}
			sub = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sub), "xargs"))
			continue
		}
		if !commandWrappers[head] {
			return sub
		}
		// Drop the wrapper token plus following options / duration args.
		rest := fields[1:]
		idx := 0
		for idx < len(rest) {
			tok := rest[idx]
			if strings.HasPrefix(tok, "-") || durationOrNumber.MatchString(tok) {
				idx++
				continue
			}
			break
		}
		sub = strings.Join(rest[idx:], " ")
	}
}

// bashSubcommands splits a command line and strips wrappers from each
// subcommand, yielding the inner commands to match rules against.
func bashSubcommands(command string) []string {
	parts := splitCompound(command)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, stripWrappers(p))
	}
	return out
}

// readOnlyCommands are the base commands Claude Code runs without a prompt in
// every mode. Mutating-capable tools (sed, sort, find with -delete/-exec,
// git non-read subcommands) are handled with extra care below.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "echo": true, "pwd": true, "head": true,
	"tail": true, "grep": true, "wc": true, "which": true, "diff": true,
	"stat": true, "du": true, "cd": true, "file": true, "true": true,
}

// readOnlyGitSubcommands are the git subcommands that do not mutate the working
// tree or remote.
var readOnlyGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "branch": true,
	"remote": true, "ls-files": true, "describe": true, "blame": true,
	"rev-parse": true, "cat-file": true, "shortlog": true, "tag": true,
}

// isReadOnlyCommand reports whether a single (wrapper-stripped) subcommand is a
// recognised read-only command. Conservative: a "find" with -delete/-exec, or
// a non-read git subcommand, is not treated as read-only.
func isReadOnlyCommand(sub string) bool {
	fields := strings.Fields(sub)
	if len(fields) == 0 {
		return false
	}
	cmd := fields[0]
	if readOnlyCommands[cmd] {
		return true
	}
	switch cmd {
	case "find":
		for _, f := range fields[1:] {
			if f == "-delete" || f == "-exec" || f == "-execdir" || f == "-fprint" {
				return false
			}
		}
		return true
	case "git":
		if len(fields) >= 2 {
			return readOnlyGitSubcommands[fields[1]]
		}
	case "command":
		// `command -v NAME` / `command -V NAME` only look NAME up and never
		// execute it, so they are read-only. Bare `command NAME args…` *runs*
		// NAME (e.g. `command rm -rf /`), so it must not be auto-allowed. Only
		// leading option tokens count — a `-v` after the name is an argument to
		// NAME, not to `command`.
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") {
				break // reached NAME; no -v/-V option means command executes it
			}
			if strings.ContainsAny(f, "vV") {
				return true
			}
		}
	}
	return false
}
