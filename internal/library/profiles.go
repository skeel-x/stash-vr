package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
	if err := os.Rename(tmp.Name(), libraryService.ProfilePath(id)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
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
