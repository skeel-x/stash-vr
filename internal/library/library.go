package library

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"stash-vr/internal/stash/gql"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Khan/genqlient/graphql"
	"golang.org/x/sync/singleflight"
)

type Service struct {
	client    atomic.Pointer[clientBox]
	vdCache   map[string]*VideoData
	muVdCache sync.RWMutex
	single    singleflight.Group
	Stats     Stats

	tagCache   map[string]*Tag
	muTagCache sync.RWMutex

	muSets   sync.Mutex
	sets     []SavedFilterSceneSet
	sections []Section
	setsAt   time.Time
	// setsGen counts ResetSections calls. A build started before a reset
	// checks it again before storing its result, so a reset that lands
	// mid-build is not overwritten by that build's now-stale result.
	setsGen uint64
}

// clientBox wraps the client so different concrete client types can be
// stored atomically.
type clientBox struct {
	c graphql.Client
}

// Client returns the current Stash client.
func (libraryService *Service) Client() graphql.Client {
	return libraryService.client.Load().c
}

// SetStashClient swaps the Stash client and drops every cache so the next
// request rebuilds the library against the new Stash.
func (libraryService *Service) SetStashClient(client graphql.Client) {
	libraryService.client.Store(&clientBox{c: client})
	libraryService.ResetCaches()
}

// ResetCaches drops cached scenes, tags and stats; the next index request
// refetches everything.
func (libraryService *Service) ResetCaches() {
	libraryService.muVdCache.Lock()
	libraryService.vdCache = make(map[string]*VideoData)
	libraryService.Stats = Stats{}
	libraryService.muVdCache.Unlock()

	libraryService.muTagCache.Lock()
	libraryService.tagCache = nil
	libraryService.muTagCache.Unlock()

	libraryService.ResetSections()
}

// ResetSections drops the cached section sets and sections so the next
// index request requeries Stash. Scene data stays cached. Incrementing
// setsGen also invalidates a build already in flight: see buildIndex.
func (libraryService *Service) ResetSections() {
	libraryService.muSets.Lock()
	libraryService.sets = nil
	libraryService.sections = nil
	libraryService.setsGen++
	libraryService.muSets.Unlock()
}

func (libraryService *Service) snapshot() map[string]*VideoData {
	libraryService.muVdCache.RLock()
	defer libraryService.muVdCache.RUnlock()
	return maps.Clone(libraryService.vdCache)
}

func NewService(client graphql.Client) *Service {
	s := &Service{
		vdCache: make(map[string]*VideoData),
	}
	s.client.Store(&clientBox{c: client})
	return s
}

func (libraryService *Service) Warmup(ctx context.Context) error {
	if _, err := libraryService.GetSections(ctx); err != nil {
		return fmt.Errorf("get sections: %w", err)
	}
	return nil
}

type Stats struct {
	Links  int
	Scenes int
}

// StatsSnapshot returns the current Stats under the same lock buildIndex
// uses to update them, so a reader never races the index build.
func (libraryService *Service) StatsSnapshot() Stats {
	libraryService.muVdCache.RLock()
	defer libraryService.muVdCache.RUnlock()
	return libraryService.Stats
}

type StudioRef struct {
	ID   string
	Name string
}

type Performer struct {
	ID         string
	Name       string
	Aliases    []string
	ImagePath  *string
	Details    *string
	Birthdate  *string
	Country    *string
	SceneCount int
	Studios    []StudioRef
}

type Studio struct {
	ID         string
	Name       string
	Aliases    []string
	ImagePath  *string
	Details    *string
	SceneCount int
}

func (libraryService *Service) GetAllSceneIDs(ctx context.Context) ([]string, error) {
	resp, err := gql.FindAllSceneIds(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindAllSceneIds: %w", err)
	}

	ids := make([]string, len(resp.FindScenes.Scenes))
	for i, scene := range resp.FindScenes.Scenes {
		ids[i] = scene.Id
	}
	return ids, nil
}

func (libraryService *Service) GetScenesByIDs(ctx context.Context, sceneIDs []string) ([]*VideoData, error) {
	if len(sceneIDs) == 0 {
		return []*VideoData{}, nil
	}

	intIDs, err := sceneIDsToInts(sceneIDs)
	if err != nil {
		return nil, err
	}

	vds, err := libraryService.fetchVideoData(ctx, intIDs)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*VideoData, len(vds))
	for _, vd := range vds {
		byID[vd.Id()] = vd
	}

	ordered := make([]*VideoData, 0, len(sceneIDs))
	for _, id := range sceneIDs {
		if vd := byID[id]; vd != nil {
			ordered = append(ordered, vd)
		}
	}
	return ordered, nil
}

func (libraryService *Service) GetPerformers(ctx context.Context) ([]Performer, error) {
	resp, err := gql.FindAllPerformers(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindAllPerformers: %w", err)
	}

	performers := make([]Performer, 0, len(resp.FindPerformers.Performers))
	for _, performer := range resp.FindPerformers.Performers {
		performers = append(performers, Performer{
			ID:         performer.Id,
			Name:       performer.Name,
			Aliases:    slices.Clone(performer.Alias_list),
			ImagePath:  performer.Image_path,
			SceneCount: performer.Scene_count,
		})
	}

	slices.SortFunc(performers, func(a Performer, b Performer) int {
		if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	})

	return performers, nil
}

func (libraryService *Service) GetPerformer(ctx context.Context, id string) (*Performer, error) {
	resp, err := gql.FindPerformer(ctx, libraryService.Client(), id)
	if err != nil {
		return nil, fmt.Errorf("FindPerformer: %w", err)
	}
	if resp.FindPerformer == nil {
		return nil, nil
	}

	performer := &Performer{
		ID:         resp.FindPerformer.Id,
		Name:       resp.FindPerformer.Name,
		Aliases:    slices.Clone(resp.FindPerformer.Alias_list),
		ImagePath:  resp.FindPerformer.Image_path,
		Details:    resp.FindPerformer.Details,
		Birthdate:  resp.FindPerformer.Birthdate,
		Country:    resp.FindPerformer.Country,
		SceneCount: resp.FindPerformer.Scene_count,
	}

	studioSeen := map[string]struct{}{}
	for _, scene := range resp.FindPerformer.Scenes {
		if scene == nil || scene.Studio == nil || scene.Studio.Id == "" {
			continue
		}
		if _, ok := studioSeen[scene.Studio.Id]; ok {
			continue
		}
		studioSeen[scene.Studio.Id] = struct{}{}
		performer.Studios = append(performer.Studios, StudioRef{ID: scene.Studio.Id, Name: scene.Studio.Name})
	}

	slices.SortFunc(performer.Studios, func(a StudioRef, b StudioRef) int {
		if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	})

	return performer, nil
}

func (libraryService *Service) GetStudios(ctx context.Context) ([]Studio, error) {
	resp, err := gql.FindAllStudios(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindAllStudios: %w", err)
	}

	studios := make([]Studio, 0, len(resp.FindStudios.Studios))
	for _, studio := range resp.FindStudios.Studios {
		studios = append(studios, Studio{
			ID:         studio.Id,
			Name:       studio.Name,
			Aliases:    slices.Clone(studio.Aliases),
			ImagePath:  studio.Image_path,
			SceneCount: studio.Scene_count,
		})
	}

	slices.SortFunc(studios, func(a Studio, b Studio) int {
		if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	})

	return studios, nil
}

func (libraryService *Service) GetStudio(ctx context.Context, id string) (*Studio, error) {
	resp, err := gql.FindStudio(ctx, libraryService.Client(), id)
	if err != nil {
		return nil, fmt.Errorf("FindStudio: %w", err)
	}
	if resp.FindStudio == nil {
		return nil, nil
	}

	studio := &Studio{
		ID:         resp.FindStudio.Id,
		Name:       resp.FindStudio.Name,
		Aliases:    slices.Clone(resp.FindStudio.Aliases),
		ImagePath:  resp.FindStudio.Image_path,
		Details:    resp.FindStudio.Details,
		SceneCount: resp.FindStudio.Scene_count,
	}

	return studio, nil
}

func sceneIDsToInts(sceneIDs []string) ([]int, error) {
	intIDs := make([]int, 0, len(sceneIDs))
	for _, id := range sceneIDs {
		parsed, err := strconv.Atoi(id)
		if err != nil {
			return nil, fmt.Errorf("invalid numeric scene id %q: %w", id, err)
		}
		intIDs = append(intIDs, parsed)
	}
	return intIDs, nil
}
