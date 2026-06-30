package configedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Config-change journal — the rollback substrate.
//
// Every write that goes through AtomicWriteFile (so: WriteSection,
// WriteAgentEntry, SetPreference, and the web-UI editor's raw save) is
// snapshotted BEFORE it overwrites the target, recording the file's prior bytes
// (or that it did not exist). RollbackHistory restores those snapshots, so a
// user can say "revert that" / "go back to the initial state" and the Helper —
// or the /rollback command — undoes the change deterministically, host-side.
//
// The journal is OFF until EnableHistory is called (the process wires it once at
// startup). With it off, hist is nil and AtomicWriteFile is byte-identical to a
// build without this feature (the no-op contract).

// HistoryEntry records the pre-change state of one config file. Entries sharing
// a BatchID belong to one logical change (a single settings operation may touch
// more than one file). In v1 each write is its own singleton batch; the BatchID
// is retained so a future StartChange/FinishChange can group multi-file ops
// without reworking rollback.
type HistoryEntry struct {
	ID      string    `json:"id"`
	BatchID string    `json:"batch_id"`
	Time    time.Time `json:"time"`
	Path    string    `json:"path"`
	Label   string    `json:"label"`
	Before  []byte    `json:"before,omitempty"` // base64 in JSON; nil when !Existed
	Existed bool      `json:"existed"`
}

// RollbackEntry describes one file restored by a rollback.
type RollbackEntry struct {
	Path   string `json:"path"`
	Label  string `json:"label"`
	Action string `json:"action"` // "restored" or "deleted"
}

// RollbackResult is the outcome of RollbackHistory.
type RollbackResult struct {
	Reverted  []RollbackEntry `json:"reverted"`
	Batches   int             `json:"batches"`   // logical changes undone
	Remaining int             `json:"remaining"` // logical changes still undoable
}

// HistoryChange is a user-facing summary of one undoable logical change.
type HistoryChange struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	Time  time.Time `json:"time"`
	Paths []string  `json:"paths"`
}

type historyStore struct {
	mu      sync.Mutex
	path    string // persistence file (written with atomicWriteRaw, never journaled)
	max     int    // cap on retained logical changes
	entries []HistoryEntry
	seq     int64
}

var hist *historyStore

// EnableHistory turns on config-change journaling, persisting to path and
// retaining at most maxBatches logical changes (older ones drop off). An empty
// path disables it (hist=nil ⇒ AtomicWriteFile is a no-op snapshot). Called once
// at process startup, before serving, so the package-global write is unraced.
func EnableHistory(path string, maxBatches int) {
	if path == "" {
		hist = nil
		return
	}
	h := &historyStore{path: path, max: maxBatches}
	h.load()
	hist = h
}

// recordHistory snapshots path's current bytes before it is overwritten with
// newData. A no-op when journaling is disabled or when the write does not change
// the file. Called from AtomicWriteFile.
func recordHistory(path string, newData []byte) {
	h := hist
	if h == nil {
		return
	}
	cur, rerr := os.ReadFile(path)
	existed := rerr == nil
	if existed && bytes.Equal(cur, newData) {
		return // identical write — nothing meaningful to undo
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := HistoryEntry{
		ID:      h.newIDLocked(),
		BatchID: h.newIDLocked(),
		Time:    time.Now(),
		Path:    path,
		Label:   labelForPath(path),
		Existed: existed,
	}
	if existed {
		e.Before = cur
	}
	h.entries = append(h.entries, e)
	h.capLocked()
	h.saveLocked()
}

// RollbackHistory undoes the most recent `batches` logical changes (batches <= 0
// undoes ALL recorded changes — "back to the initial state"). Each affected file
// is restored to its oldest pre-change bytes within the reverted range (or
// deleted when it did not exist before any of them). Returns what changed.
//
// The restore writes bypass the journal (atomicWriteRaw), so a rollback is not
// itself recorded — there is no "redo".
func RollbackHistory(batches int) (RollbackResult, error) {
	h := hist
	if h == nil {
		return RollbackResult{}, fmt.Errorf("settings history is not enabled")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.entries) == 0 {
		return RollbackResult{}, fmt.Errorf("no settings changes to undo")
	}

	// Distinct batch ids in chronological order.
	var order []string
	seen := map[string]bool{}
	for _, e := range h.entries {
		if !seen[e.BatchID] {
			seen[e.BatchID] = true
			order = append(order, e.BatchID)
		}
	}
	n := batches
	if n <= 0 || n > len(order) {
		n = len(order)
	}
	target := map[string]bool{}
	for _, b := range order[len(order)-n:] {
		target[b] = true
	}

	// Per affected path the OLDEST reverted entry holds the state to restore to
	// (so two edits of one file in the reverted range land on the earliest).
	oldestByPath := map[string]HistoryEntry{}
	var pathOrder []string
	for _, e := range h.entries {
		if !target[e.BatchID] {
			continue
		}
		if _, ok := oldestByPath[e.Path]; !ok {
			oldestByPath[e.Path] = e
			pathOrder = append(pathOrder, e.Path)
		}
	}

	var res RollbackResult
	for _, p := range pathOrder {
		e := oldestByPath[p]
		if e.Existed {
			if err := atomicWriteRaw(e.Path, e.Before); err != nil {
				return res, fmt.Errorf("restore %s: %w", e.Path, err)
			}
			res.Reverted = append(res.Reverted, RollbackEntry{Path: e.Path, Label: e.Label, Action: "restored"})
		} else {
			if err := os.Remove(e.Path); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("remove %s: %w", e.Path, err)
			}
			res.Reverted = append(res.Reverted, RollbackEntry{Path: e.Path, Label: e.Label, Action: "deleted"})
		}
	}
	res.Batches = n

	// Drop the reverted batches and report what remains.
	var kept []HistoryEntry
	rem := map[string]bool{}
	for _, e := range h.entries {
		if target[e.BatchID] {
			continue
		}
		kept = append(kept, e)
		rem[e.BatchID] = true
	}
	h.entries = kept
	res.Remaining = len(rem)
	h.saveLocked()
	return res, nil
}

// History returns the undoable logical changes, newest first.
func History() []HistoryChange {
	h := hist
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	type agg struct {
		label string
		t     time.Time
		paths []string
	}
	var order []string
	m := map[string]*agg{}
	for _, e := range h.entries {
		a := m[e.BatchID]
		if a == nil {
			a = &agg{label: e.Label, t: e.Time}
			m[e.BatchID] = a
			order = append(order, e.BatchID)
		}
		a.paths = append(a.paths, e.Path)
		if e.Time.After(a.t) {
			a.t = e.Time
		}
	}
	out := make([]HistoryChange, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		a := m[order[i]]
		out = append(out, HistoryChange{ID: order[i], Label: a.label, Time: a.t, Paths: a.paths})
	}
	return out
}

func (h *historyStore) newIDLocked() string {
	h.seq++
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(h.seq, 36)
}

func (h *historyStore) capLocked() {
	if h.max <= 0 {
		return
	}
	seen := map[string]bool{}
	var order []string
	for _, e := range h.entries {
		if !seen[e.BatchID] {
			seen[e.BatchID] = true
			order = append(order, e.BatchID)
		}
	}
	if len(order) <= h.max {
		return
	}
	drop := map[string]bool{}
	for _, b := range order[:len(order)-h.max] {
		drop[b] = true
	}
	var kept []HistoryEntry
	for _, e := range h.entries {
		if !drop[e.BatchID] {
			kept = append(kept, e)
		}
	}
	h.entries = kept
}

func (h *historyStore) load() {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return
	}
	var f struct {
		Entries []HistoryEntry `json:"entries"`
	}
	if json.Unmarshal(data, &f) == nil {
		h.entries = f.Entries
	}
}

func (h *historyStore) saveLocked() {
	f := struct {
		Entries []HistoryEntry `json:"entries"`
	}{Entries: h.entries}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return
	}
	_ = atomicWriteRaw(h.path, data)
}

// labelForPath derives a short human label for a config file from its path.
func labelForPath(path string) string {
	base := filepath.Base(path)
	switch base {
	case "agent.json":
		return "agent: " + filepath.Base(filepath.Dir(path))
	case "instruction.md":
		return "agent: " + filepath.Base(filepath.Dir(path)) + " (instruction)"
	case "agents.json":
		return "agents & squads"
	case "models.json":
		return "models"
	case "permissions.json":
		return "permissions"
	case "mcp_config.json":
		return "MCP servers"
	case "a2a_config.json":
		return "A2A peers"
	case "hooks.json":
		return "hooks"
	case "preferences.json":
		return "preferences"
	}
	return base
}
