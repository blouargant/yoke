package compress

import (
	"strconv"

	"github.com/cespare/xxhash/v2"
	lru "github.com/hashicorp/golang-lru/v2"
	"google.golang.org/genai"
)

// summaryCacheSize bounds per-session summary entries. 16 covers many
// re-compression rounds while keeping memory tiny (each entry is just a
// summary string).
const summaryCacheSize = 16

// summaryCache wraps an LRU keyed by middle-range hashes. Nil-safe: a nil
// cache simply returns no hits and ignores writes.
type summaryCache struct {
	c *lru.Cache[uint64, string]
}

func newSummaryCache() *summaryCache {
	c, err := lru.New[uint64, string](summaryCacheSize)
	if err != nil {
		return &summaryCache{}
	}
	return &summaryCache{c: c}
}

func (s *summaryCache) get(key uint64) (string, bool) {
	if s == nil || s.c == nil {
		return "", false
	}
	return s.c.Get(key)
}

func (s *summaryCache) put(key uint64, value string) {
	if s == nil || s.c == nil {
		return
	}
	s.c.Add(key, value)
}

// summaryKey computes a deterministic 64-bit key for a (middle, head, recent)
// triple. Boundary sizes are included so a config change invalidates entries.
func summaryKey(middle []*genai.Content, keepHead, keepRecent int) uint64 {
	h := xxhash.New()
	_, _ = h.WriteString("h=" + strconv.Itoa(keepHead) + ";r=" + strconv.Itoa(keepRecent) + ";n=" + strconv.Itoa(len(middle)) + ";")
	_, _ = h.WriteString(renderTranscript(middle))
	return h.Sum64()
}
