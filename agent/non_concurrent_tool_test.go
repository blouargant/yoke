package agent

import (
	"strings"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

type blockingRunnableTool struct {
	started chan struct{}
	release chan struct{}
}

func (toolImpl *blockingRunnableTool) Name() string { return "investigator" }

func (toolImpl *blockingRunnableTool) Description() string { return "test sub-agent" }

func (toolImpl *blockingRunnableTool) IsLongRunning() bool { return false }

func (toolImpl *blockingRunnableTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{Name: toolImpl.Name()}
}

func (toolImpl *blockingRunnableTool) Run(tool.Context, any) (map[string]any, error) {
	close(toolImpl.started)
	<-toolImpl.release
	return map[string]any{"result": "done"}, nil
}

func TestNonConcurrentToolRejectsDuplicateCallWhileRunning(t *testing.T) {
	inner := &blockingRunnableTool{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	wrapped := newNonConcurrentTool(inner).(runnableTool)
	done := make(chan error, 1)

	go func() {
		_, err := wrapped.Run(nil, map[string]any{"request": "first"})
		done <- err
	}()
	<-inner.started

	_, err := wrapped.Run(nil, map[string]any{"request": "second"})
	if err == nil {
		t.Fatal("duplicate Run() error = nil, want already-running error")
	}
	if !strings.Contains(err.Error(), `sub-agent "investigator" is already running`) {
		t.Fatalf("duplicate Run() error = %q, want already-running message", err)
	}

	close(inner.release)
	if err := <-done; err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
}

// TestNonConcurrentToolProcessRequestPacksItself guards the dispatch contract:
// ADK calls the object stored in req.Tools, so the wrapper must register
// ITSELF (not the inner agenttool) or the mutex would be bypassed at dispatch.
func TestNonConcurrentToolProcessRequestPacksItself(t *testing.T) {
	inner := &blockingRunnableTool{started: make(chan struct{}), release: make(chan struct{})}
	wrapped := newNonConcurrentTool(inner)

	req := &model.LLMRequest{}
	if err := wrapped.(interface {
		ProcessRequest(tool.Context, *model.LLMRequest) error
	}).ProcessRequest(nil, req); err != nil {
		t.Fatalf("ProcessRequest() error = %v", err)
	}
	got, ok := req.Tools["investigator"]
	if !ok {
		t.Fatalf("req.Tools missing 'investigator': %#v", req.Tools)
	}
	if got != tool.Tool(wrapped) {
		t.Fatalf("req.Tools[investigator] = %T, want the wrapper itself (mutex would be bypassed otherwise)", got)
	}
}
