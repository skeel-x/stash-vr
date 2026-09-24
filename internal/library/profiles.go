package library

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"stash-vr/internal/config"
)

// MaxProfileBytes bounds a HereSphere profile written back by the player.
const MaxProfileBytes = 1 << 20

var errBadProfileId = errors.New("profile id must be a scene id")

func profileDir() string {
	return filepath.Join(config.Application().ConfigPath, "hsp")
}

func validProfileId(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ProfilePath is where the HereSphere profile for scene id is stored.
func (libraryService *Service) ProfilePath(id string) string {
	return filepath.Join(profileDir(), "scene-"+id+".hsp")
}

// HasProfile reports whether a profile is stored for scene id.
func (libraryService *Service) HasProfile(id string) bool {
	if !validProfileId(id) {
		return false
	}
	info, err := os.Stat(libraryService.ProfilePath(id))
	return err == nil && info.Mode().IsRegular()
}

// SaveProfile stores data as the profile for scene id, atomically.
func (libraryService *Service) SaveProfile(id string, data []byte) error {
	if !validProfileId(id) {
		return errBadProfileId
	}
	if len(data) > MaxProfileBytes {
		return fmt.Errorf("profile for scene %s is %d bytes, above the %d byte limit", id, len(data), MaxProfileBytes)
	}
	dir := profileDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "scene-"+id+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	keepPreviousProfile(dir, libraryService.ProfilePath(id), id, data)
	if err := os.Rename(tmp.Name(), libraryService.ProfilePath(id)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// profileHistoryKeep is how many earlier versions of a scene's profile are
// kept under hsp/history when HereSphere saves a new one.
const profileHistoryKeep = 10

// keepPreviousProfile moves the profile about to be replaced into
// hsp/history (unless the new one is identical) and prunes old versions,
// so a save in the headset never loses the previous settings. Failures only
// cost the history entry, never the save.
func keepPreviousProfile(dir, current, id string, next []byte) {
	prev, err := os.ReadFile(current)
	if err != nil || bytes.Equal(prev, next) {
		return
	}
	histDir := filepath.Join(dir, "history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		return
	}
	name := fmt.Sprintf("scene-%s-%d.hsp", id, time.Now().UnixNano())
	if err := os.WriteFile(filepath.Join(histDir, name), prev, 0o600); err != nil {
		return
	}
	entries, err := os.ReadDir(histDir)
	if err != nil {
		return
	}
	var mine []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "scene-"+id+"-") && strings.HasSuffix(e.Name(), ".hsp") {
			mine = append(mine, e.Name())
		}
	}
	sort.Strings(mine)
	for len(mine) > profileHistoryKeep {
		_ = os.Remove(filepath.Join(histDir, mine[0]))
		mine = mine[1:]
	}
}

// ListProfiles returns the scene ids with a stored profile, numerically
// sorted.
func (libraryService *Service) ListProfiles() []string {
	entries, err := os.ReadDir(profileDir())
	if err != nil {
		return []string{}
	}
	ids := []string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "scene-") || !strings.HasSuffix(name, ".hsp") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "scene-"), ".hsp")
		if validProfileId(id) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, _ := strconv.Atoi(ids[i])
		b, _ := strconv.Atoi(ids[j])
		return a < b
	})
	return ids
}

// Where a scene's HereSphere profile comes from.
const (
	ProfileOwn       = "own"       // stored for the scene itself
	ProfileRule      = "rule"      // stored for the scene a matching rule names
	ProfileGenerated = "generated" // generated from the rules' settings
	ProfileNone      = "none"
)

// ProfileSourceFor picks the profile for scene id with resolved format f:
// its own stored profile first, else the stored profile of the scene the
// rules name, else a generated one when the rules ask for it. scene is the
// id the profile is stored or served under, empty for none. A nil has
// means no profile is stored.
func ProfileSourceFor(id string, f Format, has func(string) bool) (source, scene string) {
	switch {
	case has != nil && has(id):
		return ProfileOwn, id
	case has != nil && f.ProfileScene != "" && has(f.ProfileScene):
		return ProfileRule, f.ProfileScene
	case f.Generated:
		return ProfileGenerated, id
	}
	return ProfileNone, ""
}
