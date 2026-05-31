package agent

import (
	"fmt"
	"sync"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

// parallelAgentTool wraps a sub-agent's agenttool so the leader can launch
// several INDEPENDENT invocations of that one sub-agent in a single tool call.
// It advertises a batch schema { tasks: [ <inner-input>, ... ] } capped at
// maxInstances and fans the tasks out concurrently, bounded by a semaphore of
// width maxInstances.
//
// It is used in place of newNonConcurrentTool when an agent's max_instances is
// > 1. For max_instances <= 1 the leader still sees the inner's single-task
// schema via nonConcurrentTool and parallelism is impossible (so the behaviour
// is byte-identical to a build without this feature).
//
// Safety: agenttool.Run is self-contained per call (it builds its own runner,
// session service and session each invocation — see
// google.golang.org/adk/tool/agenttool), and we wrap with the default
// agenttool.Config (SkipSummarization=false), so no write to the shared tool
// context happens. The only object shared across the goroutines is the
// stateless llmagent behind inner, which the ADK runner is designed to invoke
// concurrently. Each goroutine reads the parent tool context's immutable state
// (UserID, State snapshot) only.
type parallelAgentTool struct {
	inner runnableTool
	max   int
}

func newParallelAgentTool(inner runnableTool, max int) tool.Tool {
	if max < 1 {
		max = 1
	}
	return &parallelAgentTool{inner: inner, max: max}
}

func (t *parallelAgentTool) Name() string { return t.inner.Name() }

func (t *parallelAgentTool) IsLongRunning() bool { return t.inner.IsLongRunning() }

func (t *parallelAgentTool) Description() string {
	return t.inner.Description() +
		fmt.Sprintf("\n\nThis tool accepts a BATCH of up to %d independent tasks in `tasks` and "+
			"runs them in parallel, returning one result per task in the same order. Use it to "+
			"dispatch several unrelated jobs at once; pass a single-element list when you only "+
			"have one task.", t.max)
}

// Declaration advertises the batch schema: an array `tasks` whose elements
// match the inner sub-agent's own input schema (the single-task shape the
// leader already knows), capped at maxInstances.
func (t *parallelAgentTool) Declaration() *genai.FunctionDeclaration {
	itemSchema := &genai.Schema{Type: genai.TypeObject}
	if inner := t.inner.Declaration(); inner != nil && inner.Parameters != nil {
		itemSchema = inner.Parameters
	}
	maxItems := int64(t.max)
	minItems := int64(1)
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"tasks": {
					Type:        genai.TypeArray,
					Items:       itemSchema,
					MinItems:    &minItems,
					MaxItems:    &maxItems,
					Description: "Independent tasks to run in parallel; each element matches a single invocation's arguments.",
				},
			},
			Required: []string{"tasks"},
		},
	}
}

// ProcessRequest packs THIS tool (not the inner agenttool) into the request so
// the model sees the batch declaration and the runner dispatches function calls
// to our Run. ADK builds its dispatch map from req.Tools, which is populated by
// whichever tool object is registered here — so delegating to the inner (as
// nonConcurrentTool does, where the schema is identical) would bypass the
// fan-out. This mirrors the unexported toolutils.PackTool.
func (t *parallelAgentTool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	return packToolDecl(req, t)
}

func (t *parallelAgentTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	margs, ok := args.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("parallel sub-agent %q expects object arguments, got %T", t.Name(), args)
	}
	raw, ok := margs["tasks"]
	if !ok {
		return nil, fmt.Errorf("parallel sub-agent %q: missing required 'tasks' array", t.Name())
	}
	taskList, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("parallel sub-agent %q: 'tasks' must be an array, got %T", t.Name(), raw)
	}
	if len(taskList) == 0 {
		return nil, fmt.Errorf("parallel sub-agent %q: 'tasks' is empty", t.Name())
	}
	if len(taskList) > t.max {
		return nil, fmt.Errorf("parallel sub-agent %q: %d tasks exceeds max_instances %d", t.Name(), len(taskList), t.max)
	}

	type slot struct {
		result map[string]any
		err    error
	}
	slots := make([]slot, len(taskList))
	sem := make(chan struct{}, t.max)
	var wg sync.WaitGroup

	for i, rawTask := range taskList {
		taskArgs, ok := rawTask.(map[string]any)
		if !ok {
			slots[i] = slot{err: fmt.Errorf("task %d: expected object arguments, got %T", i, rawTask)}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, taskArgs map[string]any) {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := t.inner.Run(ctx, taskArgs)
			slots[i] = slot{result: res, err: err}
		}(i, taskArgs)
	}
	wg.Wait()

	results := make([]any, len(slots))
	for i, s := range slots {
		if s.err != nil {
			results[i] = map[string]any{"error": s.err.Error()}
			continue
		}
		results[i] = s.result
	}
	return map[string]any{"results": results}, nil
}

// declaredTool is a tool that also advertises a function declaration — the
// shape ADK's request packing needs.
type declaredTool interface {
	tool.Tool
	Declaration() *genai.FunctionDeclaration
}

// packToolDecl replicates google.golang.org/adk/internal/toolinternal/toolutils.PackTool
// (unexported) for a single tool: it registers the tool for dispatch under its
// name in req.Tools and appends its function declaration to req.Config.Tools.
func packToolDecl(req *model.LLMRequest, t declaredTool) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	name := t.Name()
	if _, ok := req.Tools[name]; ok {
		return fmt.Errorf("duplicate tool: %q", name)
	}
	req.Tools[name] = t

	decl := t.Declaration()
	if decl == nil {
		return nil
	}
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	var funcTool *genai.Tool
	for _, ft := range req.Config.Tools {
		if ft != nil && ft.FunctionDeclarations != nil {
			funcTool = ft
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{decl},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, decl)
	}
	return nil
}
