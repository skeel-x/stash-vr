package library

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"stash-vr/internal/stash/gql"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

var ErrSceneNotFound = errors.New("scene not found")

// smartRandomID is the section id of the built-in Random smart section.
const smartRandomID = "smart:random"

// RandomScenes returns up to n distinct scenes drawn from the smart Random
// section when it is enabled, else from every section in the index.
func (libraryService *Service) RandomScenes(ctx context.Context, n int) ([]*VideoData, error) {
	sections, err := libraryService.GetSections(ctx)
	if err != nil {
		return nil, err
	}
	var pool []string
	for i := range sections {
		if sections[i].ID == smartRandomID {
			pool = sections[i].Ids
			break
		}
	}
	if len(pool) == 0 {
		seen := map[string]struct{}{}
		for i := range sections {
			for _, id := range sections[i].Ids {
				if _, dup := seen[id]; !dup {
					seen[id] = struct{}{}
					pool = append(pool, id)
				}
			}
		}
	}
	pool = slices.Clone(pool)
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	out := make([]*VideoData, 0, min(n, len(pool)))
	for _, id := range pool {
		if len(out) == n {
			break
		}
		vd, err := libraryService.GetScene(ctx, id, false)
		if err != nil {
			log.Ctx(ctx).Debug().Err(err).Str("scene", id).Msg("Random strip: scene skipped")
			continue
		}
		out = append(out, vd)
	}
	return out, nil
}

func (libraryService *Service) GetScenes(ctx context.Context) (map[string]*VideoData, error) {
	// Nothing has built the index yet (for example Playa's first request
	// after a restart): build it so the cache knows which scenes exist.
	libraryService.muVdCache.RLock()
	empty := len(libraryService.vdCache) == 0
	libraryService.muVdCache.RUnlock()
	if empty {
		if _, err := libraryService.GetSections(ctx); err != nil {
			return nil, err
		}
	}

	res, err, _ := libraryService.single.Do("scenes", func() (interface{}, error) {
		start := time.Now()
		libraryService.muVdCache.RLock()
		toFetch := make([]int, 0, len(libraryService.vdCache))
		for k, vd := range libraryService.vdCache {
			if vd == nil {
				id, _ := strconv.Atoi(k)
				toFetch = append(toFetch, id)
			}
		}
		libraryService.muVdCache.RUnlock()

		if len(toFetch) > 0 {
			vds, err := libraryService.fetchVideoData(ctx, toFetch)
			if err != nil {
				return nil, err
			}

			libraryService.muVdCache.Lock()
			for _, vd := range vds {
				libraryService.vdCache[vd.Id()] = vd
			}
			libraryService.muVdCache.Unlock()
			elapsed := time.Since(start)
			log.Ctx(ctx).Trace().Int("fetched", len(toFetch)).Dur("ms", elapsed).Msg("Updated cache")
		} else {
			log.Ctx(ctx).Trace().Msg("Cache hit, no scenes to fetch")
		}
		return libraryService.snapshot(), nil
	})
	if err != nil {
		return nil, err
	}
	return res.(map[string]*VideoData), nil
}

func (libraryService *Service) GetScene(ctx context.Context, id string, forceFetch bool) (*VideoData, error) {
	if !forceFetch {
		libraryService.muVdCache.RLock()
		vd := libraryService.vdCache[id]
		libraryService.muVdCache.RUnlock()
		if vd != nil {
			log.Ctx(ctx).Trace().Str("id", id).Msg("Return scene from cache")
			return vd, nil
		}
	}
	iid, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a scene id", ErrSceneNotFound, id)
	}
	vds, err := libraryService.fetchVideoData(ctx, []int{iid})
	if err != nil {
		return nil, err
	}
	if len(vds) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrSceneNotFound, id)
	}

	libraryService.muVdCache.Lock()
	libraryService.vdCache[id] = vds[0]
	libraryService.muVdCache.Unlock()
	log.Ctx(ctx).Trace().Str("id", id).Msg("Return scene from fetch")
	return vds[0], nil
}

func (libraryService *Service) fetchVideoData(ctx context.Context, sceneIds []int) ([]*VideoData, error) {
	resp, err := gql.FindScenes(ctx, libraryService.Client(), sceneIds)
	if err != nil {
		return nil, fmt.Errorf("FindScenes: %w", err)
	}
	vds := make([]*VideoData, len(resp.FindScenes.Scenes))
	for i, s := range resp.FindScenes.Scenes {
		vd := VideoData{SceneParts: &s.SceneParts}
		if vd.stashDate() == "" {
			vd.releaseDate = libraryService.LookedUpDate(s.Id)
		}
		libraryService.decorateTags(&vd)
		vds[i] = &vd
	}
	return vds, nil
}
