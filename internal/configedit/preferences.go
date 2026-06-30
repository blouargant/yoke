package configedit

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/blouargant/omnis/internal/paths"
)

// PreferencesPath returns the absolute path of the web-UI preferences file. It
// is always anchored under the write root ($OMNIS_HOME), never a lower-precedence
// layer — preferences are mutable user state, not shippable config. The HTTP
// server's preferencesStore points at the same file, so a chat-driven change and
// a Settings-panel change stay consistent.
func PreferencesPath() string {
	return filepath.Join(paths.ConfigWriteDir(), "preferences.json")
}

// ReadPreferences returns the parsed preferences map (theme/locale/notifications
// and anything else stored there). An absent or unparsable file yields an empty
// map, never an error — matching the server's tolerant load.
func ReadPreferences() map[string]any {
	out := map[string]any{}
	data, err := os.ReadFile(PreferencesPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// SetPreference merges a single key/value into preferences.json (load → set →
// save), so updating one preference never clobbers the others. A nil value
// deletes the key.
func SetPreference(key string, value any) error {
	cur := ReadPreferences()
	if value == nil {
		delete(cur, key)
	} else {
		cur[key] = value
	}
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(PreferencesPath()), 0o755); err != nil {
		return err
	}
	// Route through AtomicWriteFile so a preference change is journaled (and thus
	// rollback-able) and written atomically, consistent with every other config.
	return AtomicWriteFile(PreferencesPath(), data)
}
