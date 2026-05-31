package agent

import (
	"fmt"
	"sync"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

type runnableTool interface {
	tool.Tool
	Declaration() *genai.FunctionDeclaration
	Run(ctx tool.Context, args any) (map[string]any, error)
}

type nonConcurrentTool struct {
	inner runnableTool
	mu    sync.Mutex
}

func newNonConcurrentTool(inner runnableTool) tool.Tool {
	return &nonConcurrentTool{inner: inner}
}

func (t *nonConcurrentTool) Name() string { return t.inner.Name() }

func (t *nonConcurrentTool) Description() string { return t.inner.Description() }

func (t *nonConcurrentTool) IsLongRunning() bool { return t.inner.IsLongRunning() }

func (t *nonConcurrentTool) Declaration() *genai.FunctionDeclaration { return t.inner.Declaration() }

// ProcessRequest packs THIS wrapper (not the inner agenttool) into the
// request. ADK builds its function-call dispatch map from req.Tools, so
// registering the inner here would make the runner call the inner's Run
// directly and bypass the mutex below — which is exactly the gap this wrapper
// is meant to close. Packing ourselves means duplicate concurrent calls to the
// same sub-agent hit Run and get rejected. The declaration is identical to the
// inner's (Declaration delegates), so the model sees no difference.
func (t *nonConcurrentTool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	return packToolDecl(req, t)
}

func (t *nonConcurrentTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	if !t.mu.TryLock() {
		return nil, fmt.Errorf("sub-agent %q is already running; wait for its result before calling it again", t.Name())
	}
	defer t.mu.Unlock()

	return t.inner.Run(ctx, args)
}
