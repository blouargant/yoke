package configedit

import (
	"os"
	"path/filepath"
	"testing"
)

// withHistory enables the journal at a temp location for one test and disables
// it afterwards (hist is a package global; tests run sequentially).
func withHistory(t *testing.T, max int) {
	t.Helper()
	dir := t.TempDir()
	EnableHistory(filepath.Join(dir, "history.json"), max)
	t.Cleanup(func() { EnableHistory("", 0) })
}

func TestHistoryRestoresPriorContent(t *testing.T) {
	withHistory(t, 50)
	f := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(f, []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(f, []byte("B")); err != nil {
		t.Fatal(err)
	}
	res, err := RollbackHistory(1)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(res.Reverted) != 1 || res.Reverted[0].Action != "restored" {
		t.Fatalf("expected one restored entry, got %+v", res.Reverted)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "A" {
		t.Fatalf("expected restored content A, got %q", got)
	}
}

func TestHistoryDeletesNewlyCreatedFile(t *testing.T) {
	withHistory(t, 50)
	f := filepath.Join(t.TempDir(), "preferences.json")
	if err := AtomicWriteFile(f, []byte("{}")); err != nil { // file did not exist before
		t.Fatal(err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("file should exist after write: %v", err)
	}
	res, err := RollbackHistory(1)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(res.Reverted) != 1 || res.Reverted[0].Action != "deleted" {
		t.Fatalf("expected one deleted entry, got %+v", res.Reverted)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("file should be gone after rollback, stat err=%v", err)
	}
}

func TestHistoryRollbackAll(t *testing.T) {
	withHistory(t, 50)
	dir := t.TempDir()
	a := filepath.Join(dir, "permissions.json")
	b := filepath.Join(dir, "hooks.json")
	_ = os.WriteFile(a, []byte("a0"), 0o644)
	_ = AtomicWriteFile(a, []byte("a1"))
	_ = AtomicWriteFile(b, []byte("b1")) // b created
	res, err := RollbackHistory(0)       // 0 = revert everything
	if err != nil {
		t.Fatalf("rollback all: %v", err)
	}
	if res.Batches != 2 || res.Remaining != 0 {
		t.Fatalf("expected 2 batches reverted / 0 remaining, got %+v", res)
	}
	if got, _ := os.ReadFile(a); string(got) != "a0" {
		t.Fatalf("a not restored: %q", got)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("b should be removed")
	}
	if len(History()) != 0 {
		t.Fatalf("history should be empty after rollback all")
	}
}

func TestHistorySkipsIdenticalWrite(t *testing.T) {
	withHistory(t, 50)
	f := filepath.Join(t.TempDir(), "a2a_config.json")
	_ = os.WriteFile(f, []byte("same"), 0o644)
	if err := AtomicWriteFile(f, []byte("same")); err != nil {
		t.Fatal(err)
	}
	if len(History()) != 0 {
		t.Fatalf("identical write should not be journaled, got %d entries", len(History()))
	}
}

func TestHistoryOldestStateAcrossTwoEdits(t *testing.T) {
	withHistory(t, 50)
	f := filepath.Join(t.TempDir(), "models.json")
	_ = os.WriteFile(f, []byte("v0"), 0o644)
	_ = AtomicWriteFile(f, []byte("v1"))
	_ = AtomicWriteFile(f, []byte("v2"))
	// Reverting both edits must land on v0 (the oldest pre-change state).
	if _, err := RollbackHistory(2); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got, _ := os.ReadFile(f); string(got) != "v0" {
		t.Fatalf("expected v0 after reverting both edits, got %q", got)
	}
}

func TestHistoryDisabledIsNoOp(t *testing.T) {
	EnableHistory("", 0)
	f := filepath.Join(t.TempDir(), "models.json")
	if err := AtomicWriteFile(f, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(f); string(got) != "x" {
		t.Fatalf("write should still work with history off, got %q", got)
	}
	if _, err := RollbackHistory(1); err == nil {
		t.Fatalf("rollback should error when history is disabled")
	}
	if History() != nil {
		t.Fatalf("History() should be nil when disabled")
	}
}

func TestHistoryCapDropsOldest(t *testing.T) {
	withHistory(t, 2)
	dir := t.TempDir()
	for i, name := range []string{"models.json", "hooks.json", "permissions.json"} {
		f := filepath.Join(dir, name)
		_ = os.WriteFile(f, []byte("o"), 0o644)
		_ = AtomicWriteFile(f, []byte{byte('a' + i)})
	}
	// Cap is 2 logical changes; the oldest (models.json) should have dropped off.
	ch := History()
	if len(ch) != 2 {
		t.Fatalf("expected 2 retained changes, got %d", len(ch))
	}
	for _, c := range ch {
		if c.Label == "models" {
			t.Fatalf("oldest change should have been capped out: %+v", ch)
		}
	}
}
