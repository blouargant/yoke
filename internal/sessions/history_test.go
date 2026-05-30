package sessions

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConversationSquadRoundTrip exercises the on-disk persistence of the
// per-session squad: writing it via SetConversationSquad and reading it
// back through LoadPersistedSessions (the path that rebuilds the session
// list after a server restart).
func TestConversationSquadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("YOKE_HOME", tmp)
	logs := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	const sid = "sess-test"

	// Step 1: record the squad and a couple of turns.
	if err := SetConversationSquad(sid, "research"); err != nil {
		t.Fatalf("SetConversationSquad: %v", err)
	}
	if err := AppendConversationTurn(sid, "hi", "hello"); err != nil {
		t.Fatalf("AppendConversationTurn: %v", err)
	}

	// Step 2: read it back through LoadPersistedSessions (which is what
	// the server uses on startup to rebuild the in-memory registry).
	got := LoadPersistedSessions()
	var meta *SessionMeta
	for _, m := range got {
		if m.ID == sid {
			meta = m
			break
		}
	}
	if meta == nil {
		t.Fatalf("session %q missing from LoadPersistedSessions(): %+v", sid, got)
	}
	if meta.Squad != "research" {
		t.Fatalf("Squad = %q, want %q", meta.Squad, "research")
	}

	// Step 3: legacy conversation files (no squad field) load with an
	// empty Squad, which the server interprets as "default" at runtime.
	legacy := filepath.Join(logs, "conversation_legacy.json")
	body := `{"turns":[{"user_text":"x","assistant_text":"y","at":"2024-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(legacy, []byte(body), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	got = LoadPersistedSessions()
	var legacyMeta *SessionMeta
	for _, m := range got {
		if m.ID == "legacy" {
			legacyMeta = m
			break
		}
	}
	if legacyMeta == nil {
		t.Fatal("legacy session missing")
	}
	if legacyMeta.Squad != "" {
		t.Fatalf("legacy Squad = %q, want empty (default at runtime)", legacyMeta.Squad)
	}

	// Step 4: SessionMeta marshals squad as JSON omitempty.
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"squad":"research"`)) {
		t.Fatalf("marshalled meta missing squad: %s", b)
	}
}

// TestArchivedFlagRoundTrip verifies the archived flag survives a "server
// restart": SetConversationArchived persists it and LoadPersistedSessions
// reads it back into the rebuilt registry, without disturbing the
// conversation turns. Clearing the flag (unarchive) round-trips too.
func TestArchivedFlagRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("YOKE_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	const sid = "archive-test"
	if err := AppendConversationTurn(sid, "hi", "hello"); err != nil {
		t.Fatalf("AppendConversationTurn: %v", err)
	}

	reloaded := func() *SessionMeta {
		for _, m := range LoadPersistedSessions() {
			if m.ID == sid {
				return m
			}
		}
		return nil
	}

	if err := SetConversationArchived(sid, true); err != nil {
		t.Fatalf("SetConversationArchived: %v", err)
	}
	meta := reloaded()
	if meta == nil {
		t.Fatalf("session %q missing after archive", sid)
	}
	if !meta.Archived {
		t.Fatalf("Archived = false after archive, want true")
	}
	if meta.Turns != 1 {
		t.Fatalf("Turns = %d after archive, want 1 (turns must be preserved)", meta.Turns)
	}

	// Unarchive round-trips back to active.
	if err := SetConversationArchived(sid, false); err != nil {
		t.Fatalf("SetConversationArchived(false): %v", err)
	}
	if meta := reloaded(); meta == nil || meta.Archived {
		t.Fatalf("Archived still set after unarchive: %+v", meta)
	}
}

// TestRegistrySetArchived verifies the in-memory flag toggles and the missing
// session case returns false.
func TestRegistrySetArchived(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("YOKE_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	reg := NewEmptyRegistry()
	m := reg.New("")
	if !reg.SetArchived(m.ID, true) {
		t.Fatalf("SetArchived(existing) = false, want true")
	}
	if got, _ := reg.Get(m.ID); got == nil || !got.Archived {
		t.Fatalf("in-memory Archived not set: %+v", got)
	}
	if reg.SetArchived("does-not-exist", true) {
		t.Fatalf("SetArchived(missing) = true, want false")
	}
}
