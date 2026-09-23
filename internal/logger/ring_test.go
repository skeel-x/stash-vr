package logger

import (
	"fmt"
	"testing"
)

func TestRing_KeepsLastLinesInOrder(t *testing.T) {
	r := NewRing(3)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(r, "line %d\n", i)
	}
	got := r.Lines(10)
	if len(got) != 3 || got[0] != "line 3" || got[2] != "line 5" {
		t.Fatalf("got %v", got)
	}
	if got := r.Lines(2); len(got) != 2 || got[0] != "line 4" {
		t.Fatalf("Lines(2) = %v", got)
	}
}

func TestRing_SplitsPartialWrites(t *testing.T) {
	r := NewRing(5)
	r.Write([]byte("ab"))
	r.Write([]byte("c\nde\n"))
	if got := r.Lines(5); len(got) != 2 || got[0] != "abc" || got[1] != "de" {
		t.Fatalf("got %v", got)
	}
}
