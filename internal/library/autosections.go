package library

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"

	"github.com/rs/zerolog/log"
)

// maxAutoSectionsPerKind caps how many studio and how many performer
// sections are generated, so a low threshold cannot flood the players.
const maxAutoSectionsPerKind = 50

// AutoSection is a section generated for a studio or performer with enough
// scenes, or for a performer the watch history features. ID is
// "studio:<id>", "performer:<id>" or "auto:morefrom:<id>"; Ids are in the
// order the grouping query returned them (newest first), or best first
// for More from <performer>.
type AutoSection struct {
	ID    string
	Name  string
	Ids   []string
	Count int
	// After is the key of the smart section this one follows while it has
	// no override row; empty keeps the default auto placement (last).
	After string
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

// autoSectionsOn reports whether any auto section family is switched on.
func autoSectionsOn(cfg *config.ApplicationConfig) bool {
	return cfg.AutoStudioMin > 0 || cfg.AutoPerformerMin > 0 || cfg.MoreFromPerformers > 0
}

// fetchAutoSections runs the grouping query when either threshold is on
// and ranks the performers when More from <performer> is on. buildIndex
// calls it once per rebuild; everyone else reads the result cached with
// the index through autoSections.
func (libraryService *Service) fetchAutoSections(ctx context.Context) ([]AutoSection, error) {
	cfg := config.Application()
	var out []AutoSection
	if cfg.AutoStudioMin > 0 || cfg.AutoPerformerMin > 0 {
		resp, err := gql.FindSceneGroupings(ctx, libraryService.Client())
		if err != nil {
			return nil, fmt.Errorf("FindSceneGroupings: %w", err)
		}
		if resp.FindScenes != nil {
			out = groupScenes(resp.FindScenes.Scenes, cfg.AutoStudioMin, cfg.AutoPerformerMin)
		}
	}
	if cfg.MoreFromPerformers > 0 {
		// A failed ranking costs these sections, not the studio and
		// performer ones.
		more, err := libraryService.moreFromPerformers(ctx, cfg.MoreFromPerformers, cfg.SmartSectionSize)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to rank performers, skipping the More from sections")
		}
		out = append(out, more...)
	}
	return out, nil
}

// autoSections returns the auto sections of the current index, building
// it if needed. Nothing is queried while every auto section family is off.
func (libraryService *Service) autoSections(ctx context.Context) ([]AutoSection, error) {
	cfg := config.Application()
	if !autoSectionsOn(&cfg) {
		return nil, nil
	}
	res, err := libraryService.index(ctx)
	if err != nil {
		return nil, err
	}
	return res.auto, nil
}

// AutoSectionCount is how many auto sections the cached index holds; 0
// before the index is built. It never builds the index.
func (libraryService *Service) AutoSectionCount() int {
	libraryService.muSets.Lock()
	defer libraryService.muSets.Unlock()
	return len(libraryService.auto)
}

// moreFromPrefix starts the id of a More from <performer> section.
const moreFromPrefix = "auto:morefrom:"

// moreFromCandidate is a performer the watch history features, how much,
// and their scenes not played yet.
type moreFromCandidate struct {
	id, name string
	score    float64
	unplayed []*recScene
}

// moreFromSections picks the n performers the watch history features most
// (see pickHistory and historyWeight: a performer scores the summed weight
// of the played history scenes they appear in; male performers are left
// out, performers of unknown gender kept) and gives each a section of
// their scenes never played, best rated first (unrated last, then newest
// first), at most size each. A performer with nothing unplayed is skipped
// without taking a slot. No history, no sections.
func moreFromSections(scenes []*recScene, now time.Time, n, size int) []AutoSection {
	if n <= 0 {
		return nil
	}
	candidates := map[string]*moreFromCandidate{}
	for _, h := range pickHistory(scenes, now) {
		if !played(h.scene) {
			continue
		}
		seen := map[string]struct{}{}
		for _, p := range h.scene.Performers {
			if p == nil || p.Gender != nil && *p.Gender == gql.GenderEnumMale {
				continue
			}
			if _, dup := seen[p.Id]; dup {
				continue
			}
			seen[p.Id] = struct{}{}
			c, ok := candidates[p.Id]
			if !ok {
				c = &moreFromCandidate{id: p.Id, name: p.Name}
				candidates[p.Id] = c
			}
			c.score += h.weight
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	for _, s := range scenes {
		if s == nil || intVal(s.Play_count) > 0 {
			continue
		}
		for _, p := range s.Performers {
			if p == nil {
				continue
			}
			if c, ok := candidates[p.Id]; ok && (len(c.unplayed) == 0 || c.unplayed[len(c.unplayed)-1] != s) {
				c.unplayed = append(c.unplayed, s)
			}
		}
	}

	ranked := make([]*moreFromCandidate, 0, len(candidates))
	for _, c := range candidates {
		if len(c.unplayed) > 0 {
			ranked = append(ranked, c)
		}
	}
	slices.SortFunc(ranked, func(a, b *moreFromCandidate) int {
		return cmp.Or(
			cmp.Compare(b.score, a.score),
			cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name)),
			cmp.Compare(a.id, b.id),
		)
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]AutoSection, len(ranked))
	for i, c := range ranked {
		// A stable sort keeps the query order (newest first) within a rating.
		slices.SortStableFunc(c.unplayed, func(a, b *recScene) int { return cmp.Compare(ratingRank(b), ratingRank(a)) })
		if len(c.unplayed) > size {
			c.unplayed = c.unplayed[:size]
		}
		ids := make([]string, len(c.unplayed))
		for j, s := range c.unplayed {
			ids[j] = s.Id
		}
		out[i] = AutoSection{ID: moreFromPrefix + c.id, Name: "More from " + c.name, Ids: ids, Count: len(ids), After: "recommended"}
	}
	return out
}

// ratingRank orders scenes by rating with unrated ones last.
func ratingRank(s *recScene) int {
	if s.Rating100 == nil {
		return -1
	}
	return *s.Rating100
}

// moreFromCache is one computed set of More from sections and what it was
// computed for.
type moreFromCache struct {
	sections []AutoSection
	at       time.Time
	n, size  int
}

// moreFromPerformers returns the More from <performer> sections, ranking
// the performers at most once per recCacheTTL (or when the count or
// section size changes), like recommendedIDs does; an empty result is
// cached too, errors are not. Scenes played since the ranking (see
// markPlayed) are left out of a cached result.
func (libraryService *Service) moreFromPerformers(ctx context.Context, n, size int) ([]AutoSection, error) {
	now := libraryService.now()

	libraryService.muRec.Lock()
	c, gen := libraryService.moreFrom, libraryService.recGen
	libraryService.muRec.Unlock()
	if c != nil && c.n == n && c.size == size && now.Sub(c.at) < recCacheTTL {
		return libraryService.moreFromWithoutPlayed(c.sections, now), nil
	}

	resp, err := gql.FindRecommendationScenes(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindRecommendationScenes: %w", err)
	}
	var scenes []*recScene
	if resp.FindScenes != nil {
		scenes = resp.FindScenes.Scenes
	}
	sections := moreFromSections(scenes, now, n, size)

	libraryService.muRec.Lock()
	if libraryService.recGen == gen {
		libraryService.moreFrom = &moreFromCache{sections: sections, at: now, n: n, size: size}
	}
	libraryService.muRec.Unlock()
	return libraryService.moreFromWithoutPlayed(sections, now), nil
}

// moreFromWithoutPlayed copies the sections without the scenes played
// since they were computed.
func (libraryService *Service) moreFromWithoutPlayed(sections []AutoSection, now time.Time) []AutoSection {
	out := make([]AutoSection, len(sections))
	for i := range sections {
		out[i] = sections[i]
		out[i].Ids = libraryService.withoutPlayed(sections[i].Ids, now)
		out[i].Count = len(out[i].Ids)
	}
	return out
}
