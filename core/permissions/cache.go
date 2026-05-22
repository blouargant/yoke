package permissions

import "sync"

// sessionApprovalCache stores "Allow once" decisions keyed by
// (sessionID, probeKey) so identical calls in the same session don't
// re-prompt. Entries persist for the lifetime of the process; sessions
// are short-lived enough that a periodic cleanup isn't worth the
// complexity. No-op when sessionID is empty (e.g. CLI mode).
type sessionApprovalCache struct {
	mu sync.RWMutex
	m  map[string]map[string]struct{}
}

func newSessionApprovalCache() *sessionApprovalCache {
	return &sessionApprovalCache{m: map[string]map[string]struct{}{}}
}

func (c *sessionApprovalCache) has(sessionID, probeKey string) bool {
	if sessionID == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if sm, ok := c.m[sessionID]; ok {
		_, ok = sm[probeKey]
		return ok
	}
	return false
}

func (c *sessionApprovalCache) add(sessionID, probeKey string) {
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sm := c.m[sessionID]
	if sm == nil {
		sm = map[string]struct{}{}
		c.m[sessionID] = sm
	}
	sm[probeKey] = struct{}{}
}

// Forget drops the session's cached approvals. Called on session end.
func (c *sessionApprovalCache) Forget(sessionID string) {
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, sessionID)
}
