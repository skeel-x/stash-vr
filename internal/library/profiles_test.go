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

func TestProfiles_KeepsPreviousVersions(t *testing.T) {
	loadConfig(t, nil)
	s := NewService(&scriptStash{})
	for i := 0; i < profileHistoryKeep+3; i++ {
		if err := s.SaveProfile("7", []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveProfile("7", []byte{byte(profileHistoryKeep + 2)}); err != nil { // identical to current: no history entry
		t.Fatal(err)
	}
	hist, _ := os.ReadDir(filepath.Join(filepath.Dir(s.ProfilePath("7")), "history"))
	if len(hist) != profileHistoryKeep {
		t.Fatalf("expected %d history versions, got %d", profileHistoryKeep, len(hist))
	}
	if got := s.ListProfiles(); len(got) != 1 || got[0] != "7" {
		t.Fatalf("history must not show up as profiles, got %v", got)
	}
	cur, _ := os.ReadFile(s.ProfilePath("7"))
	if len(cur) != 1 || cur[0] != byte(profileHistoryKeep+2) {
		t.Fatalf("current profile wrong: %v", cur)
	}
}
