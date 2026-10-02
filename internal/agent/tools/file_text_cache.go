package tools

import (
	"container/list"
	"sync"
)

// fileTextCacheMaxBytes bounds the in-process cache of decoded file text shared by read_file and
// grep_files. Paging through a large file is several calls in a row; without a cache each one
// would download the whole object again.
const fileTextCacheMaxBytes = 64 << 20

// fileTextCache is a small byte-bounded LRU of split file lines. Keys combine the attachment ID
// and its storage key, both immutable for an upload, so entries never go stale; ownership is
// checked by the caller before any lookup.
type fileTextCache struct {
	mu       sync.Mutex
	maxBytes int
	curBytes int
	order    *list.List
	items    map[string]*list.Element
}

type fileTextCacheEntry struct {
	key   string
	lines []string
	size  int
}

func newFileTextCache(maxBytes int) *fileTextCache {
	return &fileTextCache{
		maxBytes: maxBytes,
		order:    list.New(),
		items:    make(map[string]*list.Element),
	}
}

func (c *fileTextCache) get(key string) ([]string, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*fileTextCacheEntry).lines, true
}

func (c *fileTextCache) put(key string, lines []string, size int) {
	if c == nil || size > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.curBytes -= el.Value.(*fileTextCacheEntry).size
		c.order.Remove(el)
		delete(c.items, key)
	}
	for c.curBytes+size > c.maxBytes && c.order.Len() > 0 {
		oldest := c.order.Back()
		entry := oldest.Value.(*fileTextCacheEntry)
		c.curBytes -= entry.size
		c.order.Remove(oldest)
		delete(c.items, entry.key)
	}
	c.items[key] = c.order.PushFront(&fileTextCacheEntry{key: key, lines: lines, size: size})
	c.curBytes += size
}
