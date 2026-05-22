package permissions

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Reloader serves a periodically refreshed Rules snapshot. It composes
// a base file with optional overlay paths (e.g. the user-owned
// permissions.json under $HOME/.yoke/config) plus an explicit in-memory
// overlay. mtime polling is used instead of fsnotify so the
// implementation works on every filesystem (NFS, virtio-fs, etc.) with
// no extra dependencies.
type Reloader struct {
	basePath     string
	overlayPaths []string
	staticOverlays []*Rules // appended after files

	interval time.Duration

	rules    atomic.Pointer[Rules]
	mtimes   map[string]time.Time
	mtimesMu sync.Mutex
	onChange []func()
}

// NewReloader builds a Reloader. basePath is the canonical
// permissions.json (typically the project-shipped one). overlayPaths
// are merged in order after the base; missing files are tolerated.
// staticOverlays are non-file rule sets (e.g. skill-contributed) appended
// last. The first call to Refresh runs synchronously so the Reloader
// has a Rules snapshot ready before Start returns.
func NewReloader(basePath string, overlayPaths []string, staticOverlays []*Rules) *Reloader {
	r := &Reloader{
		basePath:       basePath,
		overlayPaths:   overlayPaths,
		staticOverlays: staticOverlays,
		interval:       3 * time.Second,
		mtimes:         map[string]time.Time{},
	}
	_ = r.Refresh()
	return r
}

// SetInterval overrides the polling period. Calls before Start take
// effect on the next iteration.
func (r *Reloader) SetInterval(d time.Duration) {
	if d > 0 {
		r.interval = d
	}
}

// OnChange registers a callback fired (synchronously, on the polling
// goroutine) whenever the rule set is swapped. Use sparingly — long
// callbacks delay subsequent checks.
func (r *Reloader) OnChange(fn func()) { r.onChange = append(r.onChange, fn) }

// Snapshot returns the current Rules. Safe for concurrent use.
func (r *Reloader) Snapshot() *Rules {
	if v := r.rules.Load(); v != nil {
		return v
	}
	return &Rules{}
}

// Refresh forces an immediate reload regardless of mtime. Returns the
// error from the first failing file, but always installs a best-effort
// snapshot so the agent never sees an empty rule set on transient
// errors.
func (r *Reloader) Refresh() error {
	return r.reload(true)
}

// Start launches the polling loop. Cancel the context to stop.
func (r *Reloader) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(r.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = r.reload(false)
			}
		}
	}()
}

// reload re-reads base + overlays if mtimes changed (or force=true) and
// publishes the merged Rules atomically.
func (r *Reloader) reload(force bool) error {
	changed := force || r.detectChange()
	if !changed {
		return nil
	}

	base, err := Load(r.basePath)
	if err != nil {
		base = &Rules{}
	}
	var overlays []*Rules
	for _, p := range r.overlayPaths {
		ov, oerr := Load(p)
		if oerr == nil && ov != nil {
			overlays = append(overlays, ov)
		} else if oerr != nil && !os.IsNotExist(oerr) && err == nil {
			err = oerr
		}
	}
	overlays = append(overlays, r.staticOverlays...)

	merged := Merge(base, overlays...)
	r.rules.Store(merged)
	for _, fn := range r.onChange {
		fn()
	}
	if err != nil {
		return fmt.Errorf("reload (partial): %w", err)
	}
	return nil
}

// detectChange returns true when any tracked file's mtime has changed
// since the last poll (a file appearing or disappearing counts).
func (r *Reloader) detectChange() bool {
	r.mtimesMu.Lock()
	defer r.mtimesMu.Unlock()

	paths := append([]string{r.basePath}, r.overlayPaths...)
	changed := false
	for _, p := range paths {
		info, err := os.Stat(p)
		var mt time.Time
		if err == nil {
			mt = info.ModTime()
		}
		if prev, ok := r.mtimes[p]; !ok || !prev.Equal(mt) {
			r.mtimes[p] = mt
			changed = true
		}
	}
	return changed
}
