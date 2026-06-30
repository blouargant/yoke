package configedit

import (
	"fmt"
	"strings"
)

// EmbedderFingerprint reduces a parsed models.json object to a stable string
// capturing the embedder identity: the embed_model_ref, the referenced model's
// id/dim, and its connection (provider kind / base_url / api_key, inherited from
// the referenced provider). When this changes between two models.json revisions,
// the change requires a full server restart (the process-wide embedder survives
// hot-reload), so a settings write can honestly report restart_required. This is
// the server-side mirror of the web UI's embedderFingerprint.
func EmbedderFingerprint(modelsParsed any) string {
	models, ok := modelsParsed.(map[string]any)
	if !ok {
		return ""
	}
	ref := strings.ToLower(strings.TrimSpace(asString(models["embed_model_ref"])))
	if ref == "" {
		return ""
	}
	m := ciLookup(models["models"], ref)
	if m == nil {
		return fmt.Sprintf("ref:%s|<missing>", ref)
	}
	prov := ciLookup(models["providers"], strings.ToLower(strings.TrimSpace(asString(m["provider_ref"]))))
	provKind, provBase, provKey := "", "", ""
	if prov != nil {
		provKind = asString(prov["kind"])
		provBase = asString(prov["base_url"])
		provKey = asString(prov["api_key"])
	}
	kind := firstNonEmpty(asString(m["provider"]), provKind)
	baseURL := firstNonEmpty(asString(m["base_url"]), provBase)
	apiKey := firstNonEmpty(asString(m["api_key"]), provKey)
	return strings.Join([]string{
		ref,
		strings.TrimSpace(kind),
		strings.TrimSpace(baseURL),
		strings.TrimSpace(apiKey),
		strings.TrimSpace(asString(m["model"])),
		asString(m["dim"]),
	}, "|")
}

// ciLookup returns the map entry for key, matched case-insensitively, as a
// map[string]any. Returns nil when the container or key is absent.
func ciLookup(container any, key string) map[string]any {
	obj, ok := container.(map[string]any)
	if !ok {
		return nil
	}
	if v, ok := obj[key]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	for k, v := range obj {
		if strings.EqualFold(k, key) {
			if m, ok := v.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		// JSON numbers decode to float64; render dims like "1024" not "1024.0".
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
