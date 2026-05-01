// Package compress implements the article's "context compression at the 92%
// threshold" (Phase 2 / s06). When the running token total of a session
// exceeds Threshold tokens, we summarise the older half of the conversation
// via a one-shot model call and write the summary to a memory file so the
// agent can refer to it.
package compress

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
)

// DefaultThreshold roughly mirrors a 92% fill of a 1M-token Gemini window.
const DefaultThreshold = 920_000

// MemoryPath is where the running summary is persisted.
const DefaultMemoryPath = ".agent_memory.md"

// Config controls compression behaviour.
type Config struct {
	// Threshold in tokens. A summarisation pass runs once cumulative
	// prompt+response token usage crosses it.
	Threshold int64
	// MemoryPath is the file the running summary is appended to when
	// MemoryPathFunc is nil. Used as a single shared file across all
	// sessions — only suitable for single-user demos.
	MemoryPath string
	// MemoryPathFunc, if set, is called per session to compute the file
	// path the summary is appended to. It receives the userID and
	// sessionID from the callback context and must return a writable file
	// path. Use this to isolate per-session memory in multi-user setups.
	MemoryPathFunc func(userID, sessionID string) string
	// LLM used to generate the summary. If nil, compression runs in
	// "stub" mode: it just notes the threshold was hit.
	LLM model.LLM
}

// sessionState holds the per-session compression bookkeeping. Each session
// has its own token counter, recent-text buffer, and busy flag so two users
// hitting the harness concurrently never cross-contaminate each other's
// summaries.
type sessionState struct {
	used       atomic.Int64
	busy       atomic.Bool
	mu         sync.Mutex
	recentText string
}

// Plugin returns an ADK plugin that watches AfterModelCallback responses,
// accumulates token usage per session, and triggers compression once a
// session crosses cfg.Threshold. The second return value is a Wait function
// that blocks until any in-flight compression goroutines have finished —
// call it before the process exits to ensure memory files are fully
// written.
func Plugin(name string, cfg Config) (*plugin.Plugin, func(), error) {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultThreshold
	}
	if cfg.MemoryPath == "" {
		cfg.MemoryPath = DefaultMemoryPath
	}
	pathFor := cfg.MemoryPathFunc
	if pathFor == nil {
		pathFor = func(_, _ string) string { return cfg.MemoryPath }
	}

	var (
		wg       sync.WaitGroup
		sessions sync.Map // sessionKey -> *sessionState
	)

	getState := func(key string) *sessionState {
		if v, ok := sessions.Load(key); ok {
			return v.(*sessionState)
		}
		v, _ := sessions.LoadOrStore(key, &sessionState{})
		return v.(*sessionState)
	}

	flush := func(ctx context.Context, st *sessionState, path string) {
		st.mu.Lock()
		text := st.recentText
		st.recentText = ""
		st.mu.Unlock()
		if cfg.LLM == nil {
			_ = appendMemory(path,
				fmt.Sprintf("[compress] threshold %d reached; summary skipped (no LLM configured).", cfg.Threshold))
			return
		}
		if text == "" {
			return
		}
		req := &model.LLMRequest{
			Contents: []*genai.Content{
				{Role: "user", Parts: []*genai.Part{{Text: "Summarise the following agent transcript in <300 words, preserving facts, file paths, decisions, and open issues:\n\n" + text}}},
			},
		}
		// Drain the streaming response into one string.
		seq := cfg.LLM.GenerateContent(ctx, req, false)
		var summary string
		for resp, err := range seq {
			if err != nil {
				_ = appendMemory(path, fmt.Sprintf("[compress] error: %v", err))
				return
			}
			if resp == nil || resp.Content == nil {
				continue
			}
			for _, p := range resp.Content.Parts {
				summary += p.Text
			}
		}
		_ = appendMemory(path, "## Compressed summary\n"+summary+"\n")
	}

	cb := func(ctx agent.CallbackContext, resp *model.LLMResponse, _ error) (*model.LLMResponse, error) {
		if resp == nil || resp.UsageMetadata == nil {
			return nil, nil
		}
		userID, sessionID := ctx.UserID(), ctx.SessionID()
		key := userID + "\x00" + sessionID
		st := getState(key)

		var n int64
		n += int64(resp.UsageMetadata.PromptTokenCount)
		n += int64(resp.UsageMetadata.CandidatesTokenCount)
		st.used.Add(n)
		if resp.Content != nil {
			st.mu.Lock()
			for _, p := range resp.Content.Parts {
				if p != nil && p.Text != "" {
					st.recentText += p.Text + "\n"
				}
			}
			st.mu.Unlock()
		}
		if st.used.Load() >= cfg.Threshold && st.busy.CompareAndSwap(false, true) {
			path := pathFor(userID, sessionID)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer st.busy.Store(false)
				defer st.used.Store(0)
				flush(context.Background(), st, path)
			}()
		}
		return nil, nil
	}
	p, err := plugin.New(plugin.Config{
		Name:               name,
		AfterModelCallback: llmagent.AfterModelCallback(cb),
	})
	return p, wg.Wait, err
}

func appendMemory(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text + "\n")
	return err
}
