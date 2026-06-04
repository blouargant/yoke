# Project Memory (`AGENT.md`)

`AGENT.md` is yoke's project-memory file — the equivalent of Claude Code's
`CLAUDE.md`. Any `AGENT.md` visible from a session's working directory is
loaded automatically and prepended to the agent's system instruction on every
turn, so it acts as persistent, always-on guidance rather than something you
have to paste into the chat.

## How it's loaded

At each turn yoke resolves `AGENT.md` against the **session's working
directory** (the same cwd the Folders panel and the `!cd` shell-escape use), so
each session picks up the project it is actually working in — even when several
sessions are rooted in different folders.

Files from every layer are **concatenated**, lowest-precedence first (the most
specific guidance, closest to your cwd, comes last):

1. **System** — `/etc/yoke/AGENT.md`
2. **User (global)** — `$YOKE_HOME/AGENT.md` (applies to every project)
3. **`.agents/` layer** — `AGENT.md` inside the project-local `.agents/`
   (or `agents/`) config directory
4. **Project** — `AGENT.md` from the repository root down to the working
   directory (so a nested `AGENT.md` adds to, and refines, the root one)

When no `AGENT.md` exists anywhere, nothing is injected and behaviour is
unchanged.

## `/init` — bootstrap a starter file

Run `/init` (web UI, TUI, or CLI) to have the agent inspect the repository and
write a starter `AGENT.md` at the project root, documenting the build/test
commands, architecture, key packages, conventions, and gotchas it finds. Review
and edit the result like any other file.

## `#` — quick one-line memory

Start a composer line with `#` to append a single note to the project
`AGENT.md` without sending anything to the agent (symmetric with the `!`
shell-escape):

```
#always run `make fmt` before committing
```

The line is appended under a `## Notes` section in the project's `AGENT.md`
(created at the repository root if the file doesn't exist yet). In the CLI this
works in both the REPL and one-shot mode; in the TUI and web UI just type it in
the composer.

## Editing it

`AGENT.md` is a plain Markdown file — edit it directly, double-click it in the
Folders panel to open it in the editor, or keep appending with `#`. Changes are
picked up on the next turn automatically (no reload needed).
