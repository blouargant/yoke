package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/blouargant/agent-toolkit/agent"
)

// configFiles holds the absolute filesystem paths of the YAML files that are
// editable from the web UI. Paths are resolved once at server startup from the
// same precedence used by agent.NewAgent and never come from the HTTP client.
type configFiles struct {
	Agent       string // config/agent.yaml
	Permissions string // config/permissions.yaml
	MCP         string // config/mcp_config.yaml
}

// path returns the absolute file path for a whitelisted name (agent /
// permissions / mcp). The boolean is false for any other name.
func (c configFiles) path(name string) (string, bool) {
	switch name {
	case "agent":
		return c.Agent, true
	case "permissions":
		return c.Permissions, true
	case "mcp":
		return c.MCP, true
	default:
		return "", false
	}
}

// resolveConfigFiles determines the absolute paths of the YAML files that
// the web UI may edit. agent.ResolveRuntimeSettings provides the same
// precedence used by agent.NewAgent (defaults → YAML → ENV → Options) so
// the editor always targets the file actually loaded by the running agent.
func resolveConfigFiles(opts agent.Options) configFiles {
	out := configFiles{
		Agent:       firstNonEmpty(strings.TrimSpace(opts.ConfigPath), "config/agent.yaml"),
		Permissions: firstNonEmpty(strings.TrimSpace(opts.PermissionsConfigPath), "config/permissions.yaml"),
		MCP:         firstNonEmpty(strings.TrimSpace(opts.MCPSConfigPath), "config/mcp_config.yaml"),
	}
	settings, err := agent.ResolveRuntimeSettings(opts)
	if err == nil {
		if strings.TrimSpace(settings.ConfigPath) != "" {
			out.Agent = settings.ConfigPath
		}
		if strings.TrimSpace(settings.PermissionsConfigPath) != "" {
			out.Permissions = settings.PermissionsConfigPath
		}
		if strings.TrimSpace(settings.MCPConfigPath) != "" {
			out.MCP = settings.MCPConfigPath
		}
	}
	if abs, err := filepath.Abs(out.Agent); err == nil {
		out.Agent = abs
	}
	if abs, err := filepath.Abs(out.Permissions); err == nil {
		out.Permissions = abs
	}
	if abs, err := filepath.Abs(out.MCP); err == nil {
		out.MCP = abs
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// configFileInfo is the JSON shape returned by /api/config/files.
type configFileInfo struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
	Exists bool     `json:"exists"`
}

// configFilePayload is the JSON shape for read/write of a single file.
type configFilePayload struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Content string    `json:"content"`
	MTime   time.Time `json:"mtime"`
}

// configWriteRequest is the request body for PUT /api/config/file/:name.
type configWriteRequest struct {
	Content string     `json:"content"`
	// MTime, when set, must match the on-disk mtime; otherwise the server
	// returns 409 Conflict (optimistic concurrency).
	MTime *time.Time `json:"mtime,omitempty"`
}

// restartCoordinator orchestrates an in-place self re-exec triggered by the
// web UI. The HTTP handler responds 202 immediately, then the goroutine
// shuts the server down and execs a fresh copy of the binary with the same
// arguments and environment.
type restartCoordinator struct {
	mu       sync.Mutex
	pending  bool
	shutdown func() // called to stop the HTTP server before exec.
}

func newRestartCoordinator(shutdown func()) *restartCoordinator {
	return &restartCoordinator{shutdown: shutdown}
}

// setShutdown installs (or replaces) the shutdown callback. Used to break
// the chicken-and-egg between the coordinator (referenced by the engine) and
// the *http.Server (created after the engine).
func (r *restartCoordinator) setShutdown(fn func()) {
	r.mu.Lock()
	r.shutdown = fn
	r.mu.Unlock()
}

// trigger schedules a restart. Subsequent calls while one is pending are
// idempotent.
func (r *restartCoordinator) trigger() {
	r.mu.Lock()
	if r.pending {
		r.mu.Unlock()
		return
	}
	r.pending = true
	shutdown := r.shutdown
	r.mu.Unlock()

	go func() {
		// Give the HTTP response a moment to flush to the browser.
		time.Sleep(200 * time.Millisecond)
		if shutdown != nil {
			shutdown()
		}
		// Resolve the executable path; fall back to os.Args[0] if the
		// kernel cannot tell us. If exec fails (e.g. binary moved), the
		// process exits and a supervisor must restart it.
		bin, err := os.Executable()
		if err != nil || bin == "" {
			bin = os.Args[0]
		}
		_ = syscall.Exec(bin, os.Args, os.Environ())
		// If we reach here, exec failed: terminate so a supervisor can
		// take over.
		os.Exit(0)
	}()
}

// registerConfigRoutes mounts the configuration editor and restart endpoints.
func registerConfigRoutes(rg *gin.RouterGroup, files configFiles, restart *restartCoordinator) {
	rg.GET("/config/files", func(c *gin.Context) {
		out := []configFileInfo{
			describeConfigFile("agent", files.Agent),
			describeConfigFile("permissions", files.Permissions),
			describeConfigFile("mcp", files.MCP),
		}
		c.JSON(http.StatusOK, gin.H{"files": out})
	})

	rg.GET("/config/file/:name", func(c *gin.Context) {
		path, ok := files.path(c.Param("name"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "unknown config file"})
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				c.JSON(http.StatusOK, configFilePayload{Name: c.Param("name"), Path: path, Content: ""})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		st, _ := os.Stat(path)
		var mtime time.Time
		if st != nil {
			mtime = st.ModTime()
		}
		c.JSON(http.StatusOK, configFilePayload{
			Name:    c.Param("name"),
			Path:    path,
			Content: string(data),
			MTime:   mtime,
		})
	})

	rg.PUT("/config/file/:name", func(c *gin.Context) {
		path, ok := files.path(c.Param("name"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "unknown config file"})
			return
		}
		var req configWriteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
		// Parse-only YAML validation: anything that round-trips through
		// yaml.Unmarshal into `any` is accepted.
		var probe any
		if err := yaml.Unmarshal([]byte(req.Content), &probe); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid YAML: %v", err)})
			return
		}
		// Optimistic mtime check.
		if req.MTime != nil {
			if st, err := os.Stat(path); err == nil {
				if !st.ModTime().Equal(*req.MTime) {
					c.JSON(http.StatusConflict, gin.H{
						"error": "file changed on disk since it was loaded",
						"mtime": st.ModTime(),
					})
					return
				}
			}
		}
		if err := atomicWriteFile(path, []byte(req.Content), 0o644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		st, _ := os.Stat(path)
		var mtime time.Time
		if st != nil {
			mtime = st.ModTime()
		}
		c.JSON(http.StatusOK, configFilePayload{
			Name:    c.Param("name"),
			Path:    path,
			Content: req.Content,
			MTime:   mtime,
		})
	})

	rg.POST("/server/restart", func(c *gin.Context) {
		if restart == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "restart not available"})
			return
		}
		restart.trigger()
		c.JSON(http.StatusAccepted, gin.H{"status": "restarting"})
	})

	// Parsed JSON view: lets the browser render structured forms without
	// shipping a YAML parser. The PUT side accepts arbitrary JSON, marshals
	// it to YAML and writes atomically. Comments and original formatting
	// are NOT preserved by this path — clients should fall back to the raw
	// /config/file/:name endpoint when fidelity matters.
	rg.GET("/config/parsed/:name", func(c *gin.Context) {
		path, ok := files.path(c.Param("name"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "unknown config file"})
			return
		}
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var parsed any
		if len(data) > 0 {
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("file is not valid YAML: %v", err)})
				return
			}
		}
		st, _ := os.Stat(path)
		var mtime time.Time
		if st != nil {
			mtime = st.ModTime()
		}
		c.JSON(http.StatusOK, gin.H{
			"name":  c.Param("name"),
			"path":  path,
			"data":  normalizeYAMLForJSON(parsed),
			"mtime": mtime,
		})
	})

	rg.PUT("/config/parsed/:name", func(c *gin.Context) {
		path, ok := files.path(c.Param("name"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "unknown config file"})
			return
		}
		var req struct {
			Data  any        `json:"data"`
			MTime *time.Time `json:"mtime,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
		out, err := yaml.Marshal(req.Data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot serialize to YAML: %v", err)})
			return
		}
		if req.MTime != nil {
			if st, err := os.Stat(path); err == nil {
				if !st.ModTime().Equal(*req.MTime) {
					c.JSON(http.StatusConflict, gin.H{
						"error": "file changed on disk since it was loaded",
						"mtime": st.ModTime(),
					})
					return
				}
			}
		}
		if err := atomicWriteFile(path, out, 0o644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		st, _ := os.Stat(path)
		var mtime time.Time
		if st != nil {
			mtime = st.ModTime()
		}
		c.JSON(http.StatusOK, gin.H{
			"name":    c.Param("name"),
			"path":    path,
			"content": string(out),
			"mtime":   mtime,
		})
	})
}

// normalizeYAMLForJSON converts map[any]any (yaml.v3 default) into
// map[string]any so the result can round-trip through encoding/json.
func normalizeYAMLForJSON(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = normalizeYAMLForJSON(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeYAMLForJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeYAMLForJSON(val)
		}
		return out
	default:
		return v
	}
}

func describeConfigFile(name, path string) configFileInfo {
	info := configFileInfo{Name: name, Path: path}
	st, err := os.Stat(path)
	if err != nil {
		return info
	}
	info.Exists = true
	info.Size = st.Size()
	info.MTime = st.ModTime()
	return info
}

// atomicWriteFile writes data to path via a sibling temp file and renames
// it into place. The temp file is removed on any failure.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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
