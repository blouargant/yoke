package configedit

import (
	"fmt"
	"strconv"
	"strings"
)

// parsePointer splits a path into tokens. It accepts RFC-6901 JSON Pointers
// ("/permissions/allow/-"), a leading-slash-less variant ("permissions/allow"),
// and a dotted variant ("permissions.allow") — the last two are conveniences for
// callers (and LLMs) that omit the leading slash. RFC-6901 escapes (~1 → "/",
// ~0 → "~") are decoded for the slash forms.
func parsePointer(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if !strings.Contains(path, "/") && strings.Contains(path, ".") {
		return strings.Split(path, ".")
	}
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")
	for i, p := range parts {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		parts[i] = p
	}
	return parts
}

// SetByPointer sets value at the given path within the parsed JSON tree, returning
// the (possibly new) root. Intermediate objects are created when missing; an
// array token "-" appends. An empty path replaces the whole document.
func SetByPointer(root any, path string, value any) (any, error) {
	return setRec(root, parsePointer(path), value)
}

func setRec(node any, tokens []string, value any) (any, error) {
	if len(tokens) == 0 {
		return value, nil
	}
	tok := tokens[0]
	switch n := node.(type) {
	case map[string]any:
		newChild, err := setRec(n[tok], tokens[1:], value)
		if err != nil {
			return nil, err
		}
		n[tok] = newChild
		return n, nil
	case []any:
		if tok == "-" {
			newChild, err := setRec(nil, tokens[1:], value)
			if err != nil {
				return nil, err
			}
			return append(n, newChild), nil
		}
		idx, err := strconv.Atoi(tok)
		if err != nil || idx < 0 || idx >= len(n) {
			return nil, fmt.Errorf("invalid array index %q (len %d)", tok, len(n))
		}
		newChild, err := setRec(n[idx], tokens[1:], value)
		if err != nil {
			return nil, err
		}
		n[idx] = newChild
		return n, nil
	case nil:
		// A "-" segment against a missing node means "append to a (new) array",
		// so seed a one-element slice rather than a map with a "-" key.
		if tok == "-" {
			newChild, err := setRec(nil, tokens[1:], value)
			if err != nil {
				return nil, err
			}
			return []any{newChild}, nil
		}
		// Otherwise create an object for the missing path segment. (Indexing a
		// brand-new array by number must be seeded explicitly with a [] value.)
		m := map[string]any{}
		newChild, err := setRec(nil, tokens[1:], value)
		if err != nil {
			return nil, err
		}
		m[tok] = newChild
		return m, nil
	default:
		return nil, fmt.Errorf("cannot descend into %T at %q", node, tok)
	}
}

// RemoveByPointer deletes the value at path within the parsed JSON tree, returning
// the (possibly new) root. It errors when the path does not exist.
func RemoveByPointer(root any, path string) (any, error) {
	tokens := parsePointer(path)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("refusing to remove the whole document (empty path)")
	}
	return removeRec(root, tokens)
}

func removeRec(node any, tokens []string) (any, error) {
	tok := tokens[0]
	last := len(tokens) == 1
	switch n := node.(type) {
	case map[string]any:
		if last {
			if _, ok := n[tok]; !ok {
				return nil, fmt.Errorf("key %q not found", tok)
			}
			delete(n, tok)
			return n, nil
		}
		child, ok := n[tok]
		if !ok {
			return nil, fmt.Errorf("path segment %q not found", tok)
		}
		newChild, err := removeRec(child, tokens[1:])
		if err != nil {
			return nil, err
		}
		n[tok] = newChild
		return n, nil
	case []any:
		idx, err := strconv.Atoi(tok)
		if err != nil || idx < 0 || idx >= len(n) {
			return nil, fmt.Errorf("invalid array index %q (len %d)", tok, len(n))
		}
		if last {
			return append(n[:idx], n[idx+1:]...), nil
		}
		newChild, err := removeRec(n[idx], tokens[1:])
		if err != nil {
			return nil, err
		}
		n[idx] = newChild
		return n, nil
	default:
		return nil, fmt.Errorf("cannot descend into %T at %q", node, tok)
	}
}
