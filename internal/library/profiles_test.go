package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfiles_SaveHasListPath(t *testing.T) {
	loadConfig(t, nil)
	s := NewService(&scriptStash{})

	if s.HasProfile("7") {
		t.Fatal("no profile yet")
	}
	if err := s.SaveProfile("7", []byte("hsp-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProfile("10", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if !s.HasProfile("7") {
		t.Fatal("expected profile 7")
	}
	data, err := os.ReadFile(s.ProfilePath("7"))
	if err != nil || string(data) != "hsp-bytes" {
		t.Fatalf("read back %q %v", data, err)
	}
	if got := s.ListProfiles(); len(got) != 2 || got[0] != "7" || got[1] != "10" {
		t.Fatalf("list = %v", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(s.ProfilePath("7"))); len(entries) != 2 {
		t.Fatalf("expected no temp files left, got %d entries", len(entries))
	}
}

func TestProfiles_RejectsBadIdsAndSize(t *testing.T) {
	loadConfig(t, nil)
	s := NewService(&scriptStash{})
	for _, id := range []string{"", "../x", "7/8", "abc"} {
		if err := s.SaveProfile(id, []byte("x")); err == nil {
			t.Errorf("id %q: expected an error", id)
		}
		if s.HasProfile(id) {
			t.Errorf("id %q: HasProfile must be false", id)
		}
	}
	if err := s.SaveProfile("7", make([]byte, MaxProfileBytes+1)); err == nil {
		t.Fatal("expected oversized profile rejected")
	}
}
