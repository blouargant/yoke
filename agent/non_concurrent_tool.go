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

type requestProcessor interface {
	ProcessRequest(ctx tool.Context, req *model.LLMRequest) error
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

func (t *nonConcurrentTool) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	processor, ok := t.inner.(requestProcessor)
	if !ok {
		return nil
	}
	return processor.ProcessRequest(ctx, req)
}

func (t *nonConcurrentTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	if !t.mu.TryLock() {
		return nil, fmt.Errorf("sub-agent %q is already running; wait for its result before calling it again", t.Name())
	}
	defer t.mu.Unlock()

	return t.inner.Run(ctx, args)
}
