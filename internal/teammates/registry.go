package teammates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// SessionRegistry maps session display names (petnames or user-assigned titles)
// to their leader mailbox addresses, enabling cross-session communication.
// The registry is file-backed for persistence across restarts; an in-memory
// cache serves reads without hitting the filesystem on every lookup.
type SessionRegistry struct {
	path  string
	mu    sync.RWMutex
	cache map[string]string // nil until first load
}

// NewSessionRegistry creates a registry backed by sessions.json inside dir.
// The file is read eagerly so subsequent Lookup calls are served from memory.
func NewSessionRegistry(dir string) *SessionRegistry {
	r := &SessionRegistry{path: filepath.Join(dir, "sessions.json")}
	// Warm the cache; ignore errors (missing file is fine on first run).
	r.mu.Lock()
	r.cache, _ = r.load()
	r.mu.Unlock()
	return r
}

// Register adds or updates an entry: name → mailbox address.
func (r *SessionRegistry) Register(name, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, _ := r.load()
	m[name] = addr
	return r.save(m)
}

// Unregister removes a name from the registry.
func (r *SessionRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, _ := r.load()
	delete(m, name)
	return r.save(m)
}

// Rename atomically moves an entry from oldName to newName, preserving the
// mailbox address. If oldName is not found the call is a no-op.
func (r *SessionRegistry) Rename(oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m, _ := r.load()
	addr, ok := m[oldName]
	if !ok {
		return nil
	}
	delete(m, oldName)
	m[newName] = addr
	return r.save(m)
}

// Lookup returns the mailbox address registered under name, if any.
func (r *SessionRegistry) Lookup(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, _ := r.load()
	addr, ok := m[name]
	return addr, ok
}

// List returns a snapshot of all registered sessions.
func (r *SessionRegistry) List() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, _ := r.load()
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// load returns the in-memory cache if populated, otherwise reads the file.
// Must be called with r.mu held.
func (r *SessionRegistry) load() (map[string]string, error) {
	if r.cache != nil {
		return r.cache, nil
	}
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}
	if err != nil {
		return make(map[string]string), err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string), err
	}
	return m, nil
}

// save writes the registry to disk and updates the in-memory cache.
// Must be called with r.mu held.
func (r *SessionRegistry) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(r.path, data, 0o644); err != nil {
		return err
	}
	r.cache = m
	return nil
}
