package coverbadge

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// RenderedSize is how many rendered covers Rendered keeps.
const RenderedSize = 500

// Rendered holds the JPEG bytes of the last covers drawn with badges or a
// heatmap, shared by the cover and Playa poster endpoints.
var Rendered = NewCache(RenderedSize)

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

// Cache is a size-bounded, least-recently-used map from key to bytes,
// safe for concurrent use.
type Cache struct {
	mu    sync.Mutex
	size  int
	order *list.List
	items map[string]*list.Element
}

type entry struct {
	key   string
	value []byte
}

// NewCache returns an empty cache holding at most size entries.
func NewCache(size int) *Cache {
	return &Cache{size: size, order: list.New(), items: make(map[string]*list.Element)}
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

// Add stores value under key, evicting the least recently used entry
// when the cache is full.
func (c *Cache) Add(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*entry).value = value
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&entry{key: key, value: value})
	for c.order.Len() > c.size {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*entry).key)
	}
}

// Len returns the number of entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Reset drops every entry.
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.items = make(map[string]*list.Element)
}
