package agent

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func copyTreeForRace(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		t.Fatalf("copy %s->%s: %v", src, dst, err)
	}
}

// TestManagerConcurrentReloadKeepsCurrent reproduces the production brick: many
// concurrent hot-reloads (a settings change while another reload's slow build is
// in flight) must never empty the instance map or nil-out Current(). Before the
// monotonic genSeq + `oldGen != nextGen` teardown guard, two reloads that picked
// the same generation number made the second delete the generation it had just
// installed → /api/squads returned null and every new chat failed.
//
// Runs against a copy of /etc/omnis; skipped where that is absent. Run with -race.
func TestManagerConcurrentReloadKeepsCurrent(t *testing.T) {
	const sysSrc = "/etc/omnis"
	if _, err := os.Stat(filepath.Join(sysSrc, "agents.json")); err != nil {
		t.Skip("no /etc/omnis to reproduce against")
	}
	tmp := t.TempDir()
	etc := filepath.Join(tmp, "etc")
	home := filepath.Join(tmp, "home")
	copyTreeForRace(t, sysSrc, etc)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMNIS_HOME", home)
	t.Setenv("OMNIS_SYSTEM_CONFIG_DIR", etc)
	t.Setenv("OMNIS_CONFIG_DIRS", home+":"+etc)
	t.Setenv("OMNIS_AGENTSKILLS_DIR", "")

	ctx := context.Background()
	opts := Options{DeferModelErrors: true} // mirror the server so builds don't need live model creds
	infra, err := BuildInfrastructure(ctx, opts)
	if err != nil {
		t.Fatalf("build infra: %v", err)
	}
	defer infra.Close()
	first, err := BuildInstance(ctx, infra, opts, 1)
	if err != nil {
		t.Fatalf("build instance: %v", err)
	}
	want := len(first.SquadNames())
	if want == 0 {
		t.Fatal("baseline has no squads — bad setup")
	}
	m := NewManager(infra, first)

	// Fire many reloads concurrently — the slow builds overlap, which is exactly
	// the window the old currentGen+1 race lived in.
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Reload(ctx, opts); err != nil {
				t.Errorf("reload: %v", err)
			}
		}()
	}
	wg.Wait()

	cur := m.Current()
	if cur == nil {
		t.Fatalf("Current() is nil after concurrent reloads — the map was emptied (the production brick)")
	}
	if got := len(cur.SquadNames()); got != want {
		t.Fatalf("current generation has %d squads, want %d", got, want)
	}
	if len(m.Generations()) == 0 {
		t.Fatalf("instance map is empty after concurrent reloads")
	}
	// New sessions must still pin to a live generation with squads.
	if pinned := m.Pin("probe"); pinned == nil || len(pinned.SquadNames()) == 0 {
		t.Fatalf("Pin() after concurrent reloads returned no usable generation")
	}
}
