package util

import "sync"

// KeyedMutex serialises work per key: Lock blocks while another caller
// holds the same key and returns the matching unlock. Keys nobody holds
// cost nothing, so it suits a set as large as a scene library. The zero
// value is ready to use.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu sync.Mutex
	// refs counts the holders and waiters, so the entry is dropped once
	// the last of them unlocks.
	refs int
}

// Lock acquires key and returns the function that releases it.
func (k *KeyedMutex) Lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l := k.locks[key]
	if l == nil {
		l = &keyedLock{}
		k.locks[key] = l
	}
	l.refs++
	k.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

// held is how many keys are held or waited for, for tests.
func (k *KeyedMutex) held() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.locks)
}
