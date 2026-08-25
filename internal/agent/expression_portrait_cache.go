package agent

import (
	"sync"
)

// expressionPortraitThumbCache caches expression-portrait thumbnail bytes for developer-context
// continuity (same icon the UI shows). Bounded FIFO eviction; safe for concurrent use.
type expressionPortraitThumbCache struct {
	mu       sync.Mutex
	maxKeys  int
	entries  map[string]cachedPortraitThumb
	keyOrder []string
}

type cachedPortraitThumb struct {
	bytes     []byte
	mediaType string
}

func newExpressionPortraitThumbCache(maxKeys int) *expressionPortraitThumbCache {
	if maxKeys <= 0 {
		return nil
	}
	return &expressionPortraitThumbCache{
		maxKeys: maxKeys,
		entries: make(map[string]cachedPortraitThumb),
	}
}

// get returns copies so callers cannot mutate cache internals.
func (c *expressionPortraitThumbCache) get(key string) ([]byte, string, bool) {
	if c == nil || key == "" {
		return nil, "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	if !ok || len(v.bytes) == 0 {
		return nil, "", false
	}
	cp := make([]byte, len(v.bytes))
	copy(cp, v.bytes)
	return cp, v.mediaType, true
}

func (c *expressionPortraitThumbCache) put(key string, bytes []byte, mediaType string) {
	if c == nil || key == "" || len(bytes) == 0 {
		return
	}
	if mediaType == "" {
		mediaType = "image/jpeg"
	}
	cp := make([]byte, len(bytes))
	copy(cp, bytes)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; !exists {
		c.keyOrder = append(c.keyOrder, key)
		if len(c.keyOrder) > c.maxKeys {
			evict := c.keyOrder[0]
			c.keyOrder = c.keyOrder[1:]
			delete(c.entries, evict)
		}
	}
	// Repeated append + slice-from-front keeps len bounded but can leave a huge backing array; compact occasionally.
	if c.maxKeys > 0 && cap(c.keyOrder) > 2*c.maxKeys && len(c.keyOrder) > 0 {
		compact := make([]string, len(c.keyOrder))
		copy(compact, c.keyOrder)
		c.keyOrder = compact
	}
	c.entries[key] = cachedPortraitThumb{bytes: cp, mediaType: mediaType}
}
