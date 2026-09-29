package util

import (
	"sync"
	"testing"
	"time"
)

func TestKeyedMutex_SameKeySerialises(t *testing.T) {
	var km KeyedMutex
	unlock := km.Lock("a")

	acquired := make(chan struct{})
	go func() {
		defer km.Lock("a")()
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("a second holder of the same key must wait")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter must get the key once it is released")
	}
}

func TestKeyedMutex_DifferentKeysDoNotWait(t *testing.T) {
	var km KeyedMutex
	unlockA := km.Lock("a")
	defer unlockA()

	acquired := make(chan struct{})
	go func() {
		defer km.Lock("b")()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("another key must not wait")
	}
}

func TestKeyedMutex_DropsUnusedKeys(t *testing.T) {
	var km KeyedMutex
	const workers = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "k" + string(rune('a'+i%3))
			for range 50 {
				unlock := km.Lock(key)
				mu.Lock()
				counts[key]++
				mu.Unlock()
				unlock()
			}
		}()
	}
	wg.Wait()
	if got := km.held(); got != 0 {
		t.Fatalf("expected every key released, %d still held", got)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != workers*50 {
		t.Fatalf("expected %d critical sections, got %d", workers*50, total)
	}
}
