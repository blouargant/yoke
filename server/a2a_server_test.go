package main

import (
	"strings"
	"testing"
	"time"

	toolkitagent "github.com/blouargant/yoke/agent"
)

// stubA2AServer builds an a2aServer wired to a real in-memory registry but
// with no manager (squad validation is skipped when manager is nil).
func stubA2AServer(t *testing.T) *a2aServer {
	t.Helper()
	reg := &registry{items: make(map[string]*SessionMeta)}
	reg.items["teaching-kite"] = &SessionMeta{
		ID:         "teaching-kite",
		UserID:     defaultUserID,
		Squad:      "research",
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
	}
	reg.items["plain-fox"] = &SessionMeta{
		ID:         "plain-fox",
		UserID:     defaultUserID,
		Squad:      "", // unset → server treats as default
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
	}
	return newA2AServer(nil, reg, nil, "")
}

func TestResolveRouting_EphemeralWhenNoSessionName(t *testing.T) {
	s := stubA2AServer(t)
	got, err := s.resolveRouting(nil, "task-123")
	if err != nil {
		t.Fatalf("resolveRouting: %v", err)
	}
	if got.Persistent {
		t.Fatal("expected ephemeral routing")
	}
	if got.SessionID != "task-123" {
		t.Fatalf("SessionID: got %q, want task-123", got.SessionID)
	}
	if got.UserID != defaultUserID {
		t.Fatalf("UserID: got %q, want %q", got.UserID, defaultUserID)
	}
	if got.Squad != toolkitagent.DefaultSquadName {
		t.Fatalf("Squad: got %q, want %q", got.Squad, toolkitagent.DefaultSquadName)
	}
}

func TestResolveRouting_SquadOnlyHonored(t *testing.T) {
	s := stubA2AServer(t)
	got, err := s.resolveRouting(map[string]any{"squad": "research"}, "task-1")
	if err != nil {
		t.Fatalf("resolveRouting: %v", err)
	}
	if got.Squad != "research" || got.Persistent {
		t.Fatalf("got %+v, want squad=research persistent=false", got)
	}
}

func TestResolveRouting_KnownSessionUsesRegistryMeta(t *testing.T) {
	s := stubA2AServer(t)
	got, err := s.resolveRouting(map[string]any{"session_name": "teaching-kite"}, "task-1")
	if err != nil {
		t.Fatalf("resolveRouting: %v", err)
	}
	if !got.Persistent {
		t.Fatal("expected persistent routing for known session")
	}
	if got.SessionID != "teaching-kite" {
		t.Fatalf("SessionID: got %q", got.SessionID)
	}
	if got.UserID != defaultUserID {
		t.Fatalf("UserID: got %q", got.UserID)
	}
	if got.Squad != "research" {
		t.Fatalf("Squad: got %q, want research (from registry)", got.Squad)
	}
	if got.Meta == nil || got.Meta.ID != "teaching-kite" {
		t.Fatal("Meta should reference the registry entry")
	}
}

func TestResolveRouting_UnknownSessionRejected(t *testing.T) {
	s := stubA2AServer(t)
	_, err := s.resolveRouting(map[string]any{"session_name": "missing-mouse"}, "task-1")
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
	if !strings.Contains(err.Error(), "unknown session") {
		t.Fatalf("error message: %v", err)
	}
}

func TestResolveRouting_SquadConflictRejected(t *testing.T) {
	s := stubA2AServer(t)
	// teaching-kite is pinned to research; asking for default is a conflict.
	_, err := s.resolveRouting(map[string]any{
		"session_name": "teaching-kite",
		"squad":        "default",
	}, "task-1")
	if err == nil {
		t.Fatal("expected error for squad/session conflict")
	}
	if !strings.Contains(err.Error(), "pinned to squad") {
		t.Fatalf("error should mention pin; got %v", err)
	}
}

func TestResolveRouting_SquadMatchingPinAccepted(t *testing.T) {
	s := stubA2AServer(t)
	got, err := s.resolveRouting(map[string]any{
		"session_name": "teaching-kite",
		"squad":        "RESEARCH", // case-insensitive match
	}, "task-1")
	if err != nil {
		t.Fatalf("resolveRouting: %v", err)
	}
	if got.Squad != "research" {
		t.Fatalf("Squad: got %q, want research", got.Squad)
	}
}

func TestResolveRouting_SessionWithoutSquadDefaults(t *testing.T) {
	s := stubA2AServer(t)
	got, err := s.resolveRouting(map[string]any{"session_name": "plain-fox"}, "task-1")
	if err != nil {
		t.Fatalf("resolveRouting: %v", err)
	}
	if got.Squad != toolkitagent.DefaultSquadName {
		t.Fatalf("Squad: got %q, want %q", got.Squad, toolkitagent.DefaultSquadName)
	}
}

func TestResolveRouting_NoRegistryRejectsSessionName(t *testing.T) {
	s := newA2AServer(nil, nil, nil, "")
	_, err := s.resolveRouting(map[string]any{"session_name": "anything"}, "task-1")
	if err == nil {
		t.Fatal("expected error when registry is unavailable")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error message: %v", err)
	}
}
