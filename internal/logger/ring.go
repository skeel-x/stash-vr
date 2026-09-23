package logger

import (
	"bytes"
	"sync"
)

// Ring keeps the last size complete log lines in memory for the web UI.
type Ring struct {
	mu      sync.Mutex
	size    int
	lines   []string
	partial []byte
}

func NewRing(size int) *Ring { return &Ring{size: size} }

// Write implements io.Writer; input is split on newlines and a trailing
// fragment is kept until the next write completes it.
func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partial = append(r.partial, p...)
	for {
		i := bytes.IndexByte(r.partial, '\n')
		if i < 0 {
			break
		}
		r.lines = append(r.lines, string(r.partial[:i]))
		r.partial = r.partial[i+1:]
	}
	if over := len(r.lines) - r.size; over > 0 {
		r.lines = append([]string(nil), r.lines[over:]...)
	}
	return len(p), nil
}

// Lines returns up to n of the most recent lines, oldest first.
func (r *Ring) Lines(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > len(r.lines) {
		n = len(r.lines)
	}
	return append([]string(nil), r.lines[len(r.lines)-n:]...)
}

// Tail is the ring the process logger writes to.
var Tail = NewRing(500)
