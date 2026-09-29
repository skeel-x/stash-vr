package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestProfiles_DeleteMovesToHistoryAndForgetsStudioIndex(t *testing.T) {
	loadConfig(t, nil)
	s := NewService(&scriptStash{})
	if err := s.SaveProfile("7", []byte("current")); err != nil {
		t.Fatal(err)
	}
	// Fill the history so the delete has to prune too.
	for i := 0; i < profileHistoryKeep; i++ {
		if err := s.SaveProfile("7", []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveProfile("7", []byte("last")); err != nil {
		t.Fatal(err)
	}
	s.studios.mu.Lock()
	s.studios.built = true
	s.studios.mu.Unlock()

	if err := s.DeleteProfile("7"); err != nil {
		t.Fatal(err)
	}

	if s.HasProfile("7") || len(s.ListProfiles()) != 0 {
		t.Fatal("expected the profile gone")
	}
	histDir := filepath.Join(filepath.Dir(s.ProfilePath("7")), "history")
	hist, _ := os.ReadDir(histDir)
	if len(hist) != profileHistoryKeep {
		t.Fatalf("expected the history pruned to %d versions, got %d", profileHistoryKeep, len(hist))
	}
	// The deleted profile is the newest version there.
	newest, _ := os.ReadFile(filepath.Join(histDir, hist[len(hist)-1].Name()))
	if string(newest) != "last" {
		t.Fatalf("expected the deleted profile as the newest history entry, got %q", newest)
	}
	s.studios.mu.Lock()
	built := s.studios.built
	s.studios.mu.Unlock()
	if built {
		t.Fatal("expected the studio index dropped")
	}

	// Gone now, and never stored, and not an id: all refused without a change.
	for _, id := range []string{"7", "8", "", "../x", "abc"} {
		err := s.DeleteProfile(id)
		if err == nil {
			t.Errorf("id %q: expected an error", id)
		}
		if validProfileId(id) && !errors.Is(err, ErrNoProfile) {
			t.Errorf("id %q: expected ErrNoProfile, got %v", id, err)
		}
	}
	if hist, _ = os.ReadDir(histDir); len(hist) != profileHistoryKeep {
		t.Fatal("a refused delete must not touch the history")
	}
}

func TestProfiles_ConcurrentSavesOfOneScene(t *testing.T) {
	loadConfig(t, nil)
	s := NewService(&scriptStash{})
	const rounds = 25

	// Two headsets (or two quick saves from one) write scene 7's profile at
	// the same time, each with its own distinct payloads.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for g := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range rounds {
				payload := []byte(fmt.Sprintf("writer-%d-round-%d", g, i))
				if err := s.SaveProfile("7", payload); err != nil {
					errs[g] = err
					return
				}
			}
		}()
	}
	wg.Wait()
	for g, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", g, err)
		}
	}

	// The stored profile is one whole payload, never a torn or empty one.
	data, err := os.ReadFile(s.ProfilePath("7"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "writer-") || !strings.Contains(string(data), "-round-") {
		t.Fatalf("stored profile is not one of the payloads: %q", data)
	}
	// Every save was serialised: no temp files are left, and history was
	// pruned to its limit rather than overshooting under two pruners.
	entries, err := os.ReadDir(filepath.Dir(s.ProfilePath("7")))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	hist, err := os.ReadDir(filepath.Join(filepath.Dir(s.ProfilePath("7")), "history"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) > profileHistoryKeep {
		t.Fatalf("history holds %d versions, want at most %d", len(hist), profileHistoryKeep)
	}
	if !s.HasProfile("7") {
		t.Fatal("expected profile 7 after the concurrent saves")
	}
}
