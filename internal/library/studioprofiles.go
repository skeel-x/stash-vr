package library

import (
	"context"
	"math"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"stash-vr/internal/config"
)

// LensKey names the lens a resolved format plays through: projection,
// lens and field of view, for example "fisheye|MKX200|200" or
// "equirectangular||0". Scenes share a learned profile only under the
// same key, so a 180 scene never gets a fisheye profile.
func LensKey(f *Format) string {
	return f.Projection + "|" + f.Lens + "|" + strconv.Itoa(int(math.Round(float64(f.Fov))))
}

// studioEntry is the scene whose profile was saved last for one studio
// and lens key, and when.
type studioEntry struct {
	scene string
	saved time.Time
}

// studioIndex maps a studio id and lens key to the scene with the newest
// saved profile. It is built on first use from the stored profiles and
// the scene data, kept until ResetCaches or a change of the video rules,
// and brought up to date with the profiles saved since.
type studioIndex struct {
	// mu guards the index and is held while it is built, which may
	// query Stash.
	mu      sync.Mutex
	built   bool
	rules   *config.VideoRule
	nRules  int
	entries map[string]studioEntry

	// pending are the scenes whose profile was saved since the last
	// lookup. It has its own lock so a save never waits for a build.
	muPending sync.Mutex
	pending   []string
}

func studioKey(studio, lens string) string {
	return studio + "\x00" + lens
}

// reset drops the index; the next lookup builds it again.
func (idx *studioIndex) reset() {
	idx.mu.Lock()
	idx.built, idx.entries, idx.rules = false, nil, nil
	idx.mu.Unlock()
}

// saved notes a profile stored for scene id.
func (idx *studioIndex) saved(id string) {
	idx.muPending.Lock()
	defer idx.muPending.Unlock()
	if !slices.Contains(idx.pending, id) {
		idx.pending = append(idx.pending, id)
	}
}

// takePending returns and clears the scenes saved since the last call.
func (idx *studioIndex) takePending() []string {
	idx.muPending.Lock()
	defer idx.muPending.Unlock()
	p := idx.pending
	idx.pending = nil
	return p
}

// studioOf is the scene's studio id, "" for none.
func studioOf(vd *VideoData) string {
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Studio == nil {
		return ""
	}
	return vd.SceneParts.Studio.Id
}

// StudioProfile is the scene whose saved HereSphere profile vd inherits
// when learn_studio_profiles is on: the most recently saved profile of
// another scene with the same studio and the same LensKey of f. It is ""
// when the setting is off, vd has no studio, no such profile exists or
// the index cannot be built.
func (libraryService *Service) StudioProfile(ctx context.Context, vd *VideoData, f *Format) string {
	cfg := config.Application()
	studio := studioOf(vd)
	if !cfg.LearnStudioProfiles || studio == "" || f == nil {
		return ""
	}
	idx := &libraryService.studios
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if err := libraryService.refreshStudioIndex(ctx, cfg.VideoRules); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Studio profiles unavailable")
		return ""
	}
	e, ok := idx.entries[studioKey(studio, LensKey(f))]
	if !ok || e.scene == vd.Id() || !libraryService.HasProfile(e.scene) {
		return ""
	}
	return e.scene
}

// refreshStudioIndex builds the index when there is none or the rules
// changed, else adds the profiles saved since. idx.mu is held.
func (libraryService *Service) refreshStudioIndex(ctx context.Context, rules []config.VideoRule) error {
	idx := &libraryService.studios
	var first *config.VideoRule
	if len(rules) > 0 {
		first = &rules[0]
	}
	// Taken before listing the profiles: a save landing during the build
	// is then picked up on the next lookup at worst twice, never missed.
	pending := idx.takePending()
	if !idx.built || idx.rules != first || idx.nRules != len(rules) {
		entries := map[string]studioEntry{}
		if err := libraryService.addStudioEntries(ctx, rules, entries, libraryService.ListProfiles()); err != nil {
			return err
		}
		idx.built, idx.rules, idx.nRules, idx.entries = true, first, len(rules), entries
		return nil
	}
	if len(pending) > 0 {
		if err := libraryService.addStudioEntries(ctx, rules, idx.entries, pending); err != nil {
			for _, id := range pending {
				idx.saved(id)
			}
			return err
		}
	}
	return nil
}

// addStudioEntries files the stored profiles of the scenes ids under
// their studio and lens key where they are newer than the entry there.
// Scenes not cached yet are fetched in one query.
func (libraryService *Service) addStudioEntries(ctx context.Context, rules []config.VideoRule, entries map[string]studioEntry, ids []string) error {
	vds, err := libraryService.scenesByID(ctx, ids)
	if err != nil {
		return err
	}
	for _, vd := range vds {
		studio := studioOf(vd)
		if studio == "" {
			continue
		}
		info, err := os.Stat(libraryService.ProfilePath(vd.Id()))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f := ResolveFormat(rules, vd.SceneParts.Tags)
		key := studioKey(studio, LensKey(&f))
		if cur, ok := entries[key]; ok && !newerSave(info.ModTime(), vd.Id(), cur) {
			continue
		}
		entries[key] = studioEntry{scene: vd.Id(), saved: info.ModTime()}
	}
	return nil
}

// newerSave reports a save at t of scene id as newer than cur; equal
// times go to the higher scene id so the pick does not depend on order.
func newerSave(t time.Time, id string, cur studioEntry) bool {
	if !t.Equal(cur.saved) {
		return t.After(cur.saved)
	}
	a, _ := strconv.Atoi(id)
	b, _ := strconv.Atoi(cur.scene)
	return a > b
}

// scenesByID returns the scenes ids from the cache, fetching the ones not
// cached in a single query and caching them. Unknown ids are left out.
func (libraryService *Service) scenesByID(ctx context.Context, ids []string) ([]*VideoData, error) {
	var out []*VideoData
	var missing []int
	libraryService.muVdCache.RLock()
	for _, id := range ids {
		if vd := libraryService.vdCache[id]; vd != nil {
			out = append(out, vd)
		} else if n, err := strconv.Atoi(id); err == nil {
			missing = append(missing, n)
		}
	}
	libraryService.muVdCache.RUnlock()
	if len(missing) == 0 {
		return out, nil
	}
	vds, err := libraryService.fetchVideoData(ctx, missing)
	if err != nil {
		return nil, err
	}
	libraryService.muVdCache.Lock()
	for _, vd := range vds {
		libraryService.vdCache[vd.Id()] = vd
	}
	libraryService.muVdCache.Unlock()
	return append(out, vds...), nil
}
