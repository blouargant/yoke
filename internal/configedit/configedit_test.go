package configedit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSetByPointer(t *testing.T) {
	root := map[string]any{"a": map[string]any{"b": 1.0}}
	got, err := SetByPointer(root, "/a/c", "x")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	m := got.(map[string]any)["a"].(map[string]any)
	if m["c"] != "x" || m["b"] != 1.0 {
		t.Fatalf("unexpected tree: %#v", got)
	}
}

func TestSetByPointerCreatesIntermediate(t *testing.T) {
	got, err := SetByPointer(map[string]any{}, "/permissions/allow/-", "Bash(kubectl get *)")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	allow := got.(map[string]any)["permissions"].(map[string]any)["allow"]
	t.Logf("allow=%#v", allow)
	// "-" appends to an array; the intermediate "allow" was created as nil, so the
	// append seeds a one-element slice.
	arr, ok := allow.([]any)
	if !ok || len(arr) != 1 || arr[0] != "Bash(kubectl get *)" {
		t.Fatalf("expected single-element allow slice, got %#v", allow)
	}
}

func TestSetByPointerAppendsToExistingArray(t *testing.T) {
	root := map[string]any{"permissions": map[string]any{"allow": []any{"a"}}}
	got, err := SetByPointer(root, "/permissions/allow/-", "b")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	arr := got.(map[string]any)["permissions"].(map[string]any)["allow"].([]any)
	if len(arr) != 2 || arr[1] != "b" {
		t.Fatalf("expected [a b], got %#v", arr)
	}
}

func TestRemoveByPointer(t *testing.T) {
	root := map[string]any{"allow": []any{"a", "b", "c"}}
	got, err := RemoveByPointer(root, "/allow/1")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	arr := got.(map[string]any)["allow"].([]any)
	if !reflect.DeepEqual(arr, []any{"a", "c"}) {
		t.Fatalf("expected [a c], got %#v", arr)
	}

	if _, err := RemoveByPointer(map[string]any{}, "/missing"); err == nil {
		t.Fatalf("expected error removing missing key")
	}
	if _, err := RemoveByPointer(root, ""); err == nil {
		t.Fatalf("expected error on empty pointer")
	}
}

func TestPointerDottedAndSlashless(t *testing.T) {
	root := map[string]any{}
	got, err := SetByPointer(root, "models.premium.model", "claude")
	if err != nil {
		t.Fatalf("set dotted: %v", err)
	}
	if got.(map[string]any)["models"].(map[string]any)["premium"].(map[string]any)["model"] != "claude" {
		t.Fatalf("dotted pointer failed: %#v", got)
	}
}

func TestEmbedderFingerprint(t *testing.T) {
	base := map[string]any{
		"embed_model_ref": "emb",
		"providers":       map[string]any{"p": map[string]any{"kind": "openai_compat", "base_url": "http://x", "api_key": "K"}},
		"models":          map[string]any{"emb": map[string]any{"provider_ref": "p", "model": "text-embed", "dim": 1024.0, "embedding": true}},
	}
	fp1 := EmbedderFingerprint(base)
	if fp1 == "" {
		t.Fatalf("expected non-empty fingerprint")
	}
	// Same content → same fingerprint.
	if fp1 != EmbedderFingerprint(deepCopy(t, base)) {
		t.Fatalf("fingerprint not stable")
	}
	// Changing the embedding model's dim changes the fingerprint.
	changed := deepCopy(t, base)
	changed["models"].(map[string]any)["emb"].(map[string]any)["dim"] = 768.0
	if EmbedderFingerprint(changed) == fp1 {
		t.Fatalf("dim change should alter fingerprint")
	}
	// No embed_model_ref → empty.
	if EmbedderFingerprint(map[string]any{"models": map[string]any{}}) != "" {
		t.Fatalf("missing embed_model_ref should give empty fingerprint")
	}
}

func TestReadWriteSectionRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OMNIS_HOME", tmp)
	t.Setenv("OMNIS_CONFIG_DIRS", tmp)

	want := map[string]any{"models": map[string]any{"premium": map[string]any{"model": "claude-x"}}}
	wpath, layer, err := WriteSection("models", want)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if layer != "user" {
		t.Fatalf("expected user layer, got %q", layer)
	}
	if filepath.Dir(wpath) != tmp {
		t.Fatalf("expected write under %s, got %s", tmp, wpath)
	}
	got, _, gotLayer, _, err := ReadSection("models")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if gotLayer != "user" {
		t.Fatalf("read layer = %q", gotLayer)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OMNIS_HOME", tmp)

	if err := SetPreference("theme", "github-dark"); err != nil {
		t.Fatalf("set theme: %v", err)
	}
	if err := SetPreference("notifications", true); err != nil {
		t.Fatalf("set notifications: %v", err)
	}
	prefs := ReadPreferences()
	if prefs["theme"] != "github-dark" || prefs["notifications"] != true {
		t.Fatalf("unexpected prefs: %#v", prefs)
	}
	// Merge semantics: setting one key preserves the other.
	if err := SetPreference("locale", "fr"); err != nil {
		t.Fatalf("set locale: %v", err)
	}
	prefs = ReadPreferences()
	if prefs["theme"] != "github-dark" || prefs["locale"] != "fr" {
		t.Fatalf("merge lost a key: %#v", prefs)
	}
	// Verify it actually wrote the canonical file.
	if _, err := os.Stat(PreferencesPath()); err != nil {
		t.Fatalf("preferences file missing: %v", err)
	}
}

func deepCopy(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(m)
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("deepcopy: %v", err)
	}
	return out
}
