package agent

import (
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

// countingRunnableTool records peak concurrency and echoes its request.
type countingRunnableTool struct {
	inFlight atomic.Int32
	peak     atomic.Int32
	hold     time.Duration
}

func (c *countingRunnableTool) Name() string        { return "investigator" }
func (c *countingRunnableTool) Description() string { return "test sub-agent" }
func (c *countingRunnableTool) IsLongRunning() bool { return false }

func (c *countingRunnableTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name: c.Name(),
		Parameters: &genai.Schema{
			Type:       genai.TypeObject,
			Properties: map[string]*genai.Schema{"request": {Type: genai.TypeString}},
			Required:   []string{"request"},
		},
	}
}

func (c *countingRunnableTool) Run(_ tool.Context, args any) (map[string]any, error) {
	n := c.inFlight.Add(1)
	for {
		p := c.peak.Load()
		if n <= p || c.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if c.hold > 0 {
		time.Sleep(c.hold)
	}
	c.inFlight.Add(-1)
	req, _ := args.(map[string]any)
	return map[string]any{"echo": req["request"]}, nil
}

func TestParallelAgentToolDeclarationIsBatch(t *testing.T) {
	pt := newParallelAgentTool(&countingRunnableTool{}, 4).(*parallelAgentTool)
	decl := pt.Declaration()
	tasks, ok := decl.Parameters.Properties["tasks"]
	if !ok {
		t.Fatalf("declaration missing 'tasks' property: %+v", decl.Parameters.Properties)
	}
	if tasks.Type != genai.TypeArray {
		t.Fatalf("tasks type = %q, want ARRAY", tasks.Type)
	}
	if tasks.MaxItems == nil || *tasks.MaxItems != 4 {
		t.Fatalf("tasks maxItems = %v, want 4", tasks.MaxItems)
	}
	if tasks.Items == nil || tasks.Items.Properties["request"] == nil {
		t.Fatalf("tasks item schema should mirror inner request schema, got %+v", tasks.Items)
	}
}

func TestParallelAgentToolRunsConcurrentlyInOrder(t *testing.T) {
	inner := &countingRunnableTool{hold: 30 * time.Millisecond}
	pt := newParallelAgentTool(inner, 3).(*parallelAgentTool)

	out, err := pt.Run(nil, map[string]any{"tasks": []any{
		map[string]any{"request": "a"},
		map[string]any{"request": "b"},
		map[string]any{"request": "c"},
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	results, ok := out["results"].([]any)
	if !ok || len(results) != 3 {
		t.Fatalf("results = %#v, want 3-element slice", out["results"])
	}
	want := []string{"a", "b", "c"}
	for i, r := range results {
		m := r.(map[string]any)
		if m["echo"] != want[i] {
			t.Fatalf("result[%d] echo = %v, want %q (order not preserved)", i, m["echo"], want[i])
		}
	}
	if peak := inner.peak.Load(); peak < 2 {
		t.Fatalf("peak concurrency = %d, want >= 2 (tasks should overlap)", peak)
	}
}

func TestParallelAgentToolRejectsTooManyTasks(t *testing.T) {
	pt := newParallelAgentTool(&countingRunnableTool{}, 2).(*parallelAgentTool)
	_, err := pt.Run(nil, map[string]any{"tasks": []any{
		map[string]any{"request": "a"},
		map[string]any{"request": "b"},
		map[string]any{"request": "c"},
	}})
	if err == nil {
		t.Fatal("Run() with 3 tasks (max 2) error = nil, want exceeds-max error")
	}
}

func TestParallelAgentToolPerTaskErrorIsolated(t *testing.T) {
	// One bad task (not an object) should not fail the whole batch; its slot
	// carries an error, the good task still runs.
	pt := newParallelAgentTool(&countingRunnableTool{}, 3).(*parallelAgentTool)
	out, err := pt.Run(nil, map[string]any{"tasks": []any{
		"not-an-object",
		map[string]any{"request": "ok"},
	}})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (per-task isolation)", err)
	}
	results := out["results"].([]any)
	if _, hasErr := results[0].(map[string]any)["error"]; !hasErr {
		t.Fatalf("result[0] = %#v, want an error map", results[0])
	}
	if results[1].(map[string]any)["echo"] != "ok" {
		t.Fatalf("result[1] = %#v, want echo=ok", results[1])
	}
}

// TestParallelAgentToolProcessRequestPacksItself guards the dispatch contract:
// ADK builds its dispatch map from req.Tools, so the parallel tool must
// register ITSELF (with the batch declaration), not the inner single-task tool.
func TestParallelAgentToolProcessRequestPacksItself(t *testing.T) {
	pt := newParallelAgentTool(&countingRunnableTool{}, 2).(*parallelAgentTool)
	req := &model.LLMRequest{}
	if err := pt.ProcessRequest(nil, req); err != nil {
		t.Fatalf("ProcessRequest() error = %v", err)
	}
	got, ok := req.Tools["investigator"]
	if !ok {
		t.Fatalf("req.Tools missing 'investigator': %#v", req.Tools)
	}
	if got != tool.Tool(pt) {
		t.Fatalf("req.Tools[investigator] = %T, want the parallel tool itself", got)
	}
	// And the packed declaration must be the batch schema.
	if len(req.Config.Tools) == 0 || len(req.Config.Tools[0].FunctionDeclarations) == 0 {
		t.Fatal("no function declaration packed")
	}
	decl := req.Config.Tools[0].FunctionDeclarations[0]
	if _, ok := decl.Parameters.Properties["tasks"]; !ok {
		t.Fatalf("packed declaration is not the batch schema: %+v", decl.Parameters.Properties)
	}
}
