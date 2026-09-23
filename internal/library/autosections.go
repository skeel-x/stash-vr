package library

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// maxAutoSectionsPerKind caps how many studio and how many performer
// sections are generated, so a low threshold cannot flood the players.
const maxAutoSectionsPerKind = 50

// AutoSection is a section generated for a studio or performer with enough
// scenes. ID is "studio:<id>" or "performer:<id>"; Ids are in the order
// the grouping query returned them (newest first).
type AutoSection struct {
	ID    string
	Name  string
	Ids   []string
	Count int
}

// autoGroup collects the scenes of one studio or performer.
type autoGroup struct {
	id   string
	name string
	ids  []string
}

// groupScenes turns the grouping query result into auto sections: every
// studio with at least studioMin scenes and every performer with at least
// performerMin scenes (a minimum of 0 turns that kind off), largest first
// and then by name, at most maxAutoSectionsPerKind of each. Studios come
// before performers; performer sections are named "@ <name>".
func groupScenes(scenes []*gql.FindSceneGroupingsFindScenesFindScenesResultTypeScenesScene, studioMin, performerMin int) []AutoSection {
	studios := map[string]*autoGroup{}
	performers := map[string]*autoGroup{}
	add := func(groups map[string]*autoGroup, id, name, sceneID string) {
		g, ok := groups[id]
		if !ok {
			g = &autoGroup{id: id, name: name}
			groups[id] = g
		}
		// A performer listed twice on one scene counts once.
		if n := len(g.ids); n > 0 && g.ids[n-1] == sceneID {
			return
		}
		g.ids = append(g.ids, sceneID)
	}
	for _, s := range scenes {
		if s == nil {
			continue
		}
		if studioMin > 0 && s.Studio != nil {
			add(studios, s.Studio.Id, s.Studio.Name, s.Id)
		}
		if performerMin > 0 {
			for _, p := range s.Performers {
				if p != nil {
					add(performers, p.Id, p.Name, s.Id)
				}
			}
		}
	}
	out := pickGroups(studios, studioMin, "studio:", "")
	return append(out, pickGroups(performers, performerMin, "performer:", "@ ")...)
}

// pickGroups keeps the groups with at least min scenes, ordered by count
// descending, then name, then id, capped at maxAutoSectionsPerKind.
func pickGroups(groups map[string]*autoGroup, minScenes int, idPrefix, namePrefix string) []AutoSection {
	if minScenes <= 0 {
		return nil
	}
	var kept []*autoGroup
	for _, g := range groups {
		if len(g.ids) >= minScenes {
			kept = append(kept, g)
		}
	}
	slices.SortFunc(kept, func(a, b *autoGroup) int {
		return cmp.Or(
			cmp.Compare(len(b.ids), len(a.ids)),
			cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name)),
			cmp.Compare(a.id, b.id),
		)
	})
	if len(kept) > maxAutoSectionsPerKind {
		kept = kept[:maxAutoSectionsPerKind]
	}
	out := make([]AutoSection, len(kept))
	for i, g := range kept {
		out[i] = AutoSection{ID: idPrefix + g.id, Name: namePrefix + g.name, Ids: g.ids, Count: len(g.ids)}
	}
	return out
}

// fetchAutoSections runs the grouping query when either threshold is on.
// buildIndex calls it once per rebuild; everyone else reads the result
// cached with the index through autoSections.
func (libraryService *Service) fetchAutoSections(ctx context.Context) ([]AutoSection, error) {
	cfg := config.Application()
	if cfg.AutoStudioMin <= 0 && cfg.AutoPerformerMin <= 0 {
		return nil, nil
	}
	resp, err := gql.FindSceneGroupings(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindSceneGroupings: %w", err)
	}
	if resp.FindScenes == nil {
		return nil, nil
	}
	return groupScenes(resp.FindScenes.Scenes, cfg.AutoStudioMin, cfg.AutoPerformerMin), nil
}

// autoSections returns the auto sections of the current index, building
// it if needed. Nothing is queried while both thresholds are 0.
func (libraryService *Service) autoSections(ctx context.Context) ([]AutoSection, error) {
	cfg := config.Application()
	if cfg.AutoStudioMin <= 0 && cfg.AutoPerformerMin <= 0 {
		return nil, nil
	}
	res, err := libraryService.index(ctx)
	if err != nil {
		return nil, err
	}
	return res.auto, nil
}
