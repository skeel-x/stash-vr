package coverbadge

import (
	"fmt"
	"sync"
	"testing"
)

func TestCache_EvictsLeastRecentlyUsed(t *testing.T) {
	c := NewCache(2)
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
	c := NewCache(3)
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
	c := NewCache(50)
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
		Rendered.Add(fmt.Sprint(i), nil)
	}
	if Rendered.Len() != RenderedSize || RenderedSize != 500 {
		t.Fatalf("expected the rendered cover cache capped at 500, got %d", Rendered.Len())
	}
	ResetCache()
	if Rendered.Len() != 0 {
		t.Fatal("ResetCache must empty the rendered cover cache")
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
