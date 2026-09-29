package coverbadge

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// RenderedSize is how many rendered covers Rendered keeps at most.
const RenderedSize = 500

// RenderedBytes bounds the memory Rendered holds, whatever the count: a
// library of large covers must not grow the cache without limit.
const RenderedBytes = 64 << 20

// Rendered holds the JPEG bytes of the last covers drawn with badges or a
// heatmap, shared by the cover and Playa poster endpoints.
var Rendered = NewCache(RenderedSize, RenderedBytes)

// ResetCache empties Rendered; called when the library caches are reset
// and when the badge settings change.
func ResetCache() { Rendered.Reset() }

// CacheKey identifies a rendered cover: the scene, its badge set and a
// digest of the source screenshot and heatmap bytes, so a new screenshot
// in Stash renders afresh.
func CacheKey(sceneID string, badges []Badge, screenshot, heatmap []byte) string {
	h := sha256.New()
	h.Write(screenshot)
	h.Write([]byte{0})
	h.Write(heatmap)
	return sceneID + "\x00" + Key(badges) + "\x00" + hex.EncodeToString(h.Sum(nil)[:16])
}

// Cache is a least-recently-used map from key to bytes, bounded by an
// entry count and a byte budget, safe for concurrent use.
type Cache struct {
	mu       sync.Mutex
	size     int
	maxBytes int64
	bytes    int64
	order    *list.List
	items    map[string]*list.Element
}

type entry struct {
	key   string
	value []byte
}

// NewCache returns an empty cache holding at most size entries and at
// most maxBytes of values; maxBytes <= 0 means no byte budget.
func NewCache(size int, maxBytes int64) *Cache {
	return &Cache{size: size, maxBytes: maxBytes, order: list.New(), items: make(map[string]*list.Element)}
}

// Get returns the value for key and marks it most recently used.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).value, true
}

// Add stores value under key, evicting the least recently used entries
// while the cache is over its count or its byte budget. A value larger
// than the whole budget is not stored (and drops the entry it replaces).
func (c *Cache) Add(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	if c.maxBytes > 0 && int64(len(value)) > c.maxBytes {
		return
	}
	c.items[key] = c.order.PushFront(&entry{key: key, value: value})
	c.bytes += int64(len(value))
	for c.order.Len() > c.size || (c.maxBytes > 0 && c.bytes > c.maxBytes) {
		c.remove(c.order.Back())
	}
}

// remove drops el; c.mu is held.
func (c *Cache) remove(el *list.Element) {
	e := el.Value.(*entry)
	c.order.Remove(el)
	delete(c.items, e.key)
	c.bytes -= int64(len(e.value))
}

// Len returns the number of entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Bytes returns the size of the values held.
func (c *Cache) Bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

// Reset drops every entry.
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.items = make(map[string]*list.Element)
	c.bytes = 0
}
