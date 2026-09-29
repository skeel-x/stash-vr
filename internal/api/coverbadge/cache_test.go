package coverbadge

import (
	"fmt"
	"sync"
	"testing"
)

func TestCache_EvictsLeastRecentlyUsed(t *testing.T) {
	c := NewCache(2, 0)
	c.Add("a", []byte("A"))
	c.Add("b", []byte("B"))
	if _, ok := c.Get("a"); !ok { // a is now the most recent
		t.Fatal("expected a cached")
	}
	c.Add("c", []byte("C"))

	if _, ok := c.Get("b"); ok {
		t.Fatal("b was least recently used and must be evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.Get(k); !ok {
			t.Fatalf("expected %s cached", k)
		}
	}
	if c.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", c.Len())
	}
}

func TestCache_AddReplacesAndReset(t *testing.T) {
	c := NewCache(3, 0)
	c.Add("a", []byte("old"))
	c.Add("a", []byte("new"))
	if v, _ := c.Get("a"); string(v) != "new" || c.Len() != 1 {
		t.Fatalf("expected the value replaced in place, got %q with %d entries", v, c.Len())
	}

	c.Reset()

	if _, ok := c.Get("a"); ok || c.Len() != 0 {
		t.Fatal("Reset must drop every entry")
	}
}

func TestCache_ConcurrentUse(t *testing.T) {
	c := NewCache(50, 0)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				k := fmt.Sprint(g, i%70)
				c.Add(k, []byte(k))
				c.Get(k)
				if i%97 == 0 {
					c.Reset()
				}
			}
		}(g)
	}
	wg.Wait()
	if c.Len() > 50 {
		t.Fatalf("cache grew past its capacity: %d", c.Len())
	}
}

func TestRendered_HoldsFiveHundredAndResets(t *testing.T) {
	t.Cleanup(ResetCache)
	for i := 0; i < RenderedSize+10; i++ {
		Rendered.Add(fmt.Sprint(i), []byte("x"))
	}
	if Rendered.Len() != RenderedSize || RenderedSize != 500 {
		t.Fatalf("expected the rendered cover cache capped at 500, got %d", Rendered.Len())
	}
	if Rendered.Bytes() != RenderedSize || RenderedBytes != 64<<20 {
		t.Fatalf("expected %d bytes held under a 64 MiB budget, got %d", RenderedSize, Rendered.Bytes())
	}
	ResetCache()
	if Rendered.Len() != 0 || Rendered.Bytes() != 0 {
		t.Fatal("ResetCache must empty the rendered cover cache")
	}
}

func TestCache_ByteBudget(t *testing.T) {
	c := NewCache(100, 10)
	c.Add("a", []byte("aaaa"))
	c.Add("b", []byte("bbbb"))
	if c.Bytes() != 8 || c.Len() != 2 {
		t.Fatalf("expected 8 bytes in 2 entries, got %d in %d", c.Bytes(), c.Len())
	}

	// c does not fit beside a and b: a, the least recently used, goes.
	c.Add("c", []byte("cccc"))
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected a evicted to make room")
	}
	if c.Bytes() != 8 || c.Len() != 2 {
		t.Fatalf("expected 8 bytes in 2 entries after eviction, got %d in %d", c.Bytes(), c.Len())
	}

	// Replacing a value accounts for the old size.
	c.Add("b", []byte("bb"))
	if v, _ := c.Get("b"); string(v) != "bb" || c.Bytes() != 6 {
		t.Fatalf("expected b replaced and 6 bytes held, got %q and %d", v, c.Bytes())
	}

	// A value over the whole budget is not held, and drops what it replaces.
	c.Add("b", make([]byte, 11))
	if _, ok := c.Get("b"); ok || c.Bytes() != 4 || c.Len() != 1 {
		t.Fatalf("an oversized value must not be cached, got %d bytes in %d entries", c.Bytes(), c.Len())
	}

	// One value the size of the budget evicts everything else.
	c.Add("d", make([]byte, 10))
	if _, ok := c.Get("c"); ok || c.Len() != 1 || c.Bytes() != 10 {
		t.Fatalf("expected d alone, got %d entries and %d bytes", c.Len(), c.Bytes())
	}

	c.Reset()
	if c.Bytes() != 0 {
		t.Fatal("Reset must zero the byte count")
	}
}

func TestCache_NoByteBudget(t *testing.T) {
	c := NewCache(2, 0)
	c.Add("a", make([]byte, 1<<20))
	c.Add("b", make([]byte, 1<<20))
	if c.Len() != 2 || c.Bytes() != 2<<20 {
		t.Fatalf("without a budget the count alone bounds the cache, got %d entries and %d bytes", c.Len(), c.Bytes())
	}
}

func TestCacheKey(t *testing.T) {
	badges := []Badge{{Kind: KindQuality, Label: "8K"}}
	base := CacheKey("1", badges, []byte("shot"), nil)
	for name, other := range map[string]string{
		"scene":   CacheKey("2", badges, []byte("shot"), nil),
		"badges":  CacheKey("1", nil, []byte("shot"), nil),
		"source":  CacheKey("1", badges, []byte("shot2"), nil),
		"heatmap": CacheKey("1", badges, []byte("shot"), []byte("heat")),
	} {
		if other == base {
			t.Errorf("a different %s must give a different key", name)
		}
	}
	if CacheKey("1", badges, []byte("shot"), nil) != base {
		t.Error("the same inputs must give the same key")
	}
}
