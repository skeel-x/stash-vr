package library

import (
	"context"
	"fmt"
	"slices"
	"stash-vr/internal/config"
	"stash-vr/internal/stash"
	"stash-vr/internal/stash/filter"
	"stash-vr/internal/stash/gql"
	"sync"

	"github.com/rs/zerolog/log"
)

type Section struct {
	Name string
	Ids  []string
}

type SavedFilterSceneSet struct {
	ID       string
	Name     string
	SceneIDs []string
}

// SectionRow is one entry on the Sections page and one candidate section
// for the index, in display order.
type SectionRow struct {
	ID         string
	SourceName string // name in Stash, or the smart section's built-in name
	Name       string // name after the user's override
	Disabled   bool
	Smart      bool
}

func (libraryService *Service) GetSections(ctx context.Context) ([]Section, error) {
	res, err, _ := libraryService.single.Do("sections", func() (interface{}, error) {
		sources, err := libraryService.getSources(ctx)
		if err != nil {
			return nil, err
		}

		var sections []Section
		if len(sources) == 0 {
			log.Ctx(ctx).Info().Msg("No saved filters or smart sections enabled, creating default section with ALL scenes")
			sections, err = libraryService.getDefaultSections(ctx)
		} else {
			var sets []SavedFilterSceneSet
			sets, err = libraryService.resolveSections(ctx, sources)
			if err == nil {
				sections = make([]Section, len(sets))
				for i, set := range sets {
					sections[i] = Section{Name: set.Name, Ids: slices.Clone(set.SceneIDs)}
				}
			}
		}
		if err != nil {
			return nil, err
		}

		libraryService.muVdCache.Lock()
		for k := range libraryService.vdCache {
			delete(libraryService.vdCache, k)
		}

		libraryService.Stats.Links = 0
		for _, v := range sections {
			libraryService.Stats.Links += len(v.Ids)
			for _, id := range v.Ids {
				libraryService.vdCache[id] = nil
			}
		}
		libraryService.Stats.Scenes = len(libraryService.vdCache)
		libraryService.muVdCache.Unlock()

		log.Ctx(ctx).Info().Int("sections", len(sections)).Int("links", libraryService.Stats.Links).
			Int("scenes", libraryService.Stats.Scenes).
			Msg("Index built")

		_ = libraryService.LoadTags(ctx)

		libraryService.muTagCache.RLock()
		tagCount := len(libraryService.tagCache)
		libraryService.muTagCache.RUnlock()
		log.Ctx(ctx).Debug().Int("tags", tagCount).Msg("Cached tags")

		return sections, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]Section), nil
}

func (libraryService *Service) getDefaultSections(ctx context.Context) ([]Section, error) {
	resp, err := gql.FindAllSceneIds(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindAllSceneIds: %w", err)
	}
	allScenesSection := Section{
		Name: "All",
		Ids:  make([]string, len(resp.FindScenes.Scenes)),
	}
	for i := range resp.FindScenes.Scenes {
		allScenesSection.Ids[i] = resp.FindScenes.Scenes[i].Id
	}
	return []Section{allScenesSection}, nil
}

func (libraryService *Service) GetSavedFilterSceneSets(ctx context.Context) ([]SavedFilterSceneSet, error) {
	sources, err := libraryService.getSources(ctx)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return []SavedFilterSceneSet{}, nil
	}
	return libraryService.resolveSections(ctx, sources)
}

// sectionRows applies the ordering rule: smart sections without an override
// first (default state), then overrides in saved order, then remaining saved
// filters in the given order.
func sectionRows(saved []gql.SavedFilterParts, smart []SmartSection, overrides []config.Filter) []SectionRow {
	overridden := make(map[string]config.Filter, len(overrides))
	for _, o := range overrides {
		overridden[o.ID] = o
	}
	smartByID := make(map[string]SmartSection, len(smart))
	for _, s := range smart {
		smartByID[s.ID()] = s
	}
	savedByID := make(map[string]gql.SavedFilterParts, len(saved))
	for _, sf := range saved {
		savedByID[sf.Id] = sf
	}

	rows := make([]SectionRow, 0, len(smart)+len(saved))
	for _, s := range smart {
		if _, ok := overridden[s.ID()]; ok {
			continue
		}
		rows = append(rows, SectionRow{ID: s.ID(), SourceName: s.Name, Name: s.Name, Disabled: !s.Default, Smart: true})
	}
	seen := make(map[string]struct{}, len(overrides))
	for _, o := range overrides {
		seen[o.ID] = struct{}{}
		if s, ok := smartByID[o.ID]; ok {
			name := o.Name
			if name == "" {
				name = s.Name
			}
			rows = append(rows, SectionRow{ID: o.ID, SourceName: s.Name, Name: name, Disabled: o.Disabled, Smart: true})
			continue
		}
		if sf, ok := savedByID[o.ID]; ok {
			name := o.Name
			if name == "" {
				name = sf.Name
			}
			rows = append(rows, SectionRow{ID: o.ID, SourceName: sf.Name, Name: name, Disabled: o.Disabled})
		}
	}
	for _, sf := range saved {
		if _, ok := seen[sf.Id]; ok {
			continue
		}
		rows = append(rows, SectionRow{ID: sf.Id, SourceName: sf.Name, Name: sf.Name})
	}
	return rows
}

// sectionSource is one enabled row resolved to what produces its scene ids.
type sectionSource struct {
	row   SectionRow
	saved *gql.SavedFilterParts
	smart *SmartSection
}

// savedFilters returns Stash's saved scene filters in the order they should
// appear when the user has no overrides: front page filters first, as before.
func (libraryService *Service) savedFilters(ctx context.Context) ([]gql.SavedFilterParts, error) {
	resp, err := gql.FindSavedSceneFilters(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("failed to find saved filters: %w", err)
	}
	if len(resp.FindSavedFilters) == 0 {
		return nil, nil
	}
	if len(config.Application().Filters) == 0 {
		return libraryService.buildFiltersByFrontpage(ctx, resp)
	}
	out := make([]gql.SavedFilterParts, len(resp.FindSavedFilters))
	for i, sf := range resp.FindSavedFilters {
		out[i] = sf.SavedFilterParts
	}
	return out, nil
}

// SectionRows lists every smart section and saved filter in display order,
// including disabled ones, for the Sections page.
func (libraryService *Service) SectionRows(ctx context.Context) ([]SectionRow, error) {
	saved, err := libraryService.savedFilters(ctx)
	if err != nil {
		return nil, err
	}
	return sectionRows(saved, smartSections(), config.Application().Filters), nil
}

func (libraryService *Service) getSources(ctx context.Context) ([]sectionSource, error) {
	saved, err := libraryService.savedFilters(ctx)
	if err != nil {
		return nil, err
	}
	smart := smartSections()
	smartByID := make(map[string]*SmartSection, len(smart))
	for i := range smart {
		smartByID[smart[i].ID()] = &smart[i]
	}
	savedByID := make(map[string]*gql.SavedFilterParts, len(saved))
	for i := range saved {
		savedByID[saved[i].Id] = &saved[i]
	}
	var sources []sectionSource
	for _, row := range sectionRows(saved, smart, config.Application().Filters) {
		if row.Disabled {
			continue
		}
		if s, ok := smartByID[row.ID]; ok {
			sources = append(sources, sectionSource{row: row, smart: s})
		} else if sf, ok := savedByID[row.ID]; ok {
			sources = append(sources, sectionSource{row: row, saved: sf})
		}
	}
	return sources, nil
}

func (libraryService *Service) resolveSections(ctx context.Context, sources []sectionSource) ([]SavedFilterSceneSet, error) {
	size := config.Application().SmartSectionSize
	sections := make([]SavedFilterSceneSet, len(sources))

	wg := sync.WaitGroup{}
	wg.Add(len(sources))
	for i, src := range sources {
		go func(i int, src sectionSource) {
			defer wg.Done()
			flog := log.Ctx(ctx).With().Str("sectionId", src.row.ID).Str("name", src.row.Name).Logger()

			var sceneFilter *gql.SceneFilterType
			var opts *gql.FindFilterType
			if src.smart != nil {
				sceneFilter, opts = src.smart.query(size)
			} else {
				converted, err := filter.SavedFilterToSceneFilter(ctx, *src.saved)
				if err != nil {
					flog.Warn().Err(err).Msg("Failed to convert filter, skipping")
					return
				}
				sceneFilter, opts = &converted.SceneFilter, &converted.FilterOpts
			}

			resp, err := gql.FindSceneIdsByFilter(ctx, libraryService.Client(), sceneFilter, opts)
			if err != nil {
				flog.Err(err).Msg("Failed to find scenes by filter, skipping")
				return
			}
			if len(resp.FindScenes.Scenes) == 0 {
				flog.Debug().Msg("Section skipped: 0 scenes")
				return
			}
			sections[i] = SavedFilterSceneSet{ID: src.row.ID, Name: src.row.Name, SceneIDs: make([]string, len(resp.FindScenes.Scenes))}
			for j, v := range resp.FindScenes.Scenes {
				sections[i].SceneIDs[j] = v.Id
			}
			flog.Debug().Int("scenes", len(sections[i].SceneIDs)).Msg("Section built")
		}(i, src)
	}
	wg.Wait()
	return slices.DeleteFunc(sections, func(s SavedFilterSceneSet) bool { return len(s.SceneIDs) == 0 }), nil
}

func (libraryService *Service) buildFiltersByFrontpage(ctx context.Context, savedFilters *gql.FindSavedSceneFiltersResponse) ([]gql.SavedFilterParts, error) {
	fpIds, err := stash.FindSavedFilterIdsByFrontPage(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("failed to find frontpage filter IDs: %w", err)
	}

	var front []gql.SavedFilterParts

	for _, id := range fpIds {
		for _, f := range savedFilters.FindSavedFilters {
			if f.Id == id {
				front = append(front, f.SavedFilterParts)
				break
			}
		}
	}

	seen := make(map[string]struct{}, len(fpIds))
	for _, id := range fpIds {
		seen[id] = struct{}{}
	}
	var rest []gql.SavedFilterParts
	for _, f := range savedFilters.FindSavedFilters {
		if _, ok := seen[f.Id]; !ok {
			rest = append(rest, f.SavedFilterParts)
		}
	}
	out := append(front, rest...)

	log.Ctx(ctx).Debug().Int("count", len(out)).Msg("Filters built by frontpage")

	return out, nil
}
