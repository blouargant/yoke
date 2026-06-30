package configedit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/blouargant/omnis/internal/paths"
)

// ConfigFileNames maps the editor's whitelisted short section names to the
// underlying JSON filenames resolved through the config search chain.
var ConfigFileNames = map[string]string{
	"agent":       "agents.json",
	"models":      "models.json",
	"permissions": "permissions.json",
	"mcp":         "mcp_config.json",
	"a2a":         "a2a_config.json",
	"hooks":       "hooks.json",
}

// FileNameForSection returns the JSON filename backing a whitelisted section
// name, and whether the name is known.
func FileNameForSection(name string) (string, bool) {
	f, ok := ConfigFileNames[name]
	return f, ok
}

// ReadPath returns the highest-precedence read path for a whitelisted section,
// resolved through the 3-layer config search chain. Empty when the file does
// not exist in any layer (a first write will fork it into the user layer).
func ReadPath(name string) (string, bool) {
	filename, ok := ConfigFileNames[name]
	if !ok {
		return "", false
	}
	return paths.FindConfig(filename), true
}

// WritePath returns the write target for a whitelisted section. For "agent" the
// body is consulted so an agents.json that references local-only items lands in
// the local layer; every other section preserves its source layer (forking
// system → user). body may be nil.
func WritePath(name string, body []byte) (string, bool) {
	filename, ok := ConfigFileNames[name]
	if !ok {
		return "", false
	}
	readPath, _ := ReadPath(name)
	var layer string
	if name == "agent" {
		layer = AgentsConfigLayer(readPath, body)
	} else {
		layer = SourceLayer(readPath)
	}
	return filepath.Join(paths.WriteDirForLayer(layer), filename), true
}

// ReadSection reads and parses a whitelisted config section. It returns the
// parsed JSON (nil when the file is absent or empty), the read path, the layer
// the file currently lives in ("local"/"user"/"system"), and the file mtime.
// A non-existent file is not an error (parsed is nil).
func ReadSection(name string) (parsed any, readPath, layer string, mtime time.Time, err error) {
	readPath, ok := ReadPath(name)
	if !ok {
		return nil, "", "", time.Time{}, fmt.Errorf("unknown config section %q", name)
	}
	layer = paths.Layer(readPath)
	data, rerr := os.ReadFile(readPath)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, readPath, layer, time.Time{}, nil
		}
		return nil, readPath, layer, time.Time{}, rerr
	}
	if len(data) > 0 {
		if uerr := json.Unmarshal(data, &parsed); uerr != nil {
			return nil, readPath, layer, time.Time{}, fmt.Errorf("%s is not valid JSON: %w", name, uerr)
		}
	}
	if st, serr := os.Stat(readPath); serr == nil {
		mtime = st.ModTime()
	}
	return parsed, readPath, layer, mtime, nil
}

// WriteSection pretty-prints data and writes it atomically to the section's
// write target (layer-aware). It returns the write path and the resolved layer.
// Use this for every section EXCEPT "agent": agents.json is fanned out into
// per-agent registry files by the server's editor and edited entry-by-entry by
// the settings tools (see agents.go).
func WriteSection(name string, data any) (writePath, layer string, err error) {
	out, merr := json.MarshalIndent(data, "", "  ")
	if merr != nil {
		return "", "", fmt.Errorf("cannot serialize %s: %w", name, merr)
	}
	out = append(out, '\n')
	writePath, ok := WritePath(name, out)
	if !ok {
		return "", "", fmt.Errorf("unknown config section %q", name)
	}
	layer = SourceLayer(func() string { p, _ := ReadPath(name); return p }())
	if name == "agent" {
		layer = AgentsConfigLayer(func() string { p, _ := ReadPath(name); return p }(), out)
	}
	if err := os.MkdirAll(filepath.Dir(writePath), 0o755); err != nil {
		return "", "", err
	}
	if err := AtomicWriteFile(writePath, out); err != nil {
		return "", "", err
	}
	return writePath, layer, nil
}

// AtomicWriteFile writes data to path via a sibling temp file and renames it
// into place. The temp file is removed on any failure. The destination's
// existing file mode is preserved when present; otherwise 0o644 is used. The
// parent directory must already exist.
//
// Before overwriting, it snapshots the target into the config-change journal
// (when EnableHistory is active) so the write can later be rolled back; with the
// journal off this is a byte-identical no-op.
func AtomicWriteFile(path string, data []byte) error {
	recordHistory(path, data)
	return atomicWriteRaw(path, data)
}

// atomicWriteRaw is AtomicWriteFile without the history snapshot — used both as
// the write engine and by the rollback restore (which must not re-journal).
func atomicWriteRaw(path string, data []byte) error {
	perm := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-cfg-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
