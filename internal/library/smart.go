package library

import (
	"context"
	"slices"
	"strings"
	"time"

	"stash-vr/internal/stash/gql"
)

// SmartSection is a computed section: a fixed Stash query instead of a saved
// filter. Users switch, order and rename them like saved filters through
// config.Filter entries whose ID is "smart:<key>".
type SmartSection struct {
	Key     string
	Name    string
	Default bool // enabled when no override entry exists for it
	// Playa lists this section as a category; Random is left out because
	// Playa has its own randomiser.
	Playa bool
	// After is the key of the smart section this one follows while it has
	// no override row, so a section added in a later release lands next to
	// a related one instead of on top of an existing order (the first row
	// is the section HereSphere opens on). Empty keeps the default placement.
	After  string
	filter gql.SceneFilterType
	sort   string
	// dynFilter, when set, builds the filter at query time instead of
	// filter: for a threshold that moves with the clock or ids that only
	// the library knows. ok false means the section has nothing to show
	// right now and is skipped quietly.
	dynFilter func(svc *Service, now time.Time) (filter gql.SceneFilterType, ok bool)
	// ids, when set, computes the section's scenes in stash-vr instead of
	// querying Stash with filter and sort.
	ids func(svc *Service, ctx context.Context, size int) ([]string, error)
}

// ForPlaya reports whether Playa should list this section as a category.
func (s SmartSection) ForPlaya() bool { return s.Playa }

const smartPrefix = "smart:"

// ID is the override id for this section.
func (s SmartSection) ID() string { return smartPrefix + s.Key }

func intCriterion(value int, modifier gql.CriterionModifier) *gql.IntCriterionInput {
	return &gql.IntCriterionInput{Value: value, Modifier: modifier}
}

func boolPtr(b bool) *bool { return &b }

// rewatchAfter is how long a scene with an o-count must have gone unplayed
// before Worth a rewatch lists it.
const rewatchAfter = 30 * 24 * time.Hour

// stashTimestamp formats t the way Stash stores timestamps, so a
// timestamp criterion compares as its columns do.
func stashTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// rewatchFilter lists scenes with an o-count last played before
// now-rewatchAfter.
func rewatchFilter(_ *Service, now time.Time) (gql.SceneFilterType, bool) {
	return gql.SceneFilterType{
		O_counter:      intCriterion(0, gql.CriterionModifierGreaterThan),
		Last_played_at: &gql.TimestampCriterionInput{Value: stashTimestamp(now.Add(-rewatchAfter)), Modifier: gql.CriterionModifierLessThan},
	}, true
}

// Tag names the tag-based sections look for; vrQualityTags manages the
// tier tags, the passthrough ones are the usual names for an alpha matte
// or chroma key.
var (
	topQualityTags    = []string{"8K", "7K"}
	topQualityExclude = []string{"Low Detail"}
	passthroughTags   = []string{"Alpha", "Chroma Key", "Passthrough"}
)

// tagIDsNamed returns the ids of the cached tags with one of the names,
// compared case-insensitively, in id order. It is nil while the tag cache
// has not been loaded, and empty when none of the names is a tag.
func (libraryService *Service) tagIDsNamed(names []string) []string {
	libraryService.muTagCache.RLock()
	defer libraryService.muTagCache.RUnlock()
	if libraryService.tagCache == nil {
		return nil
	}
	ids := []string{}
	for _, t := range libraryService.tagCache {
		if t == nil {
			continue
		}
		for _, n := range names {
			if strings.EqualFold(t.Name, n) {
				ids = append(ids, t.Id)
				break
			}
		}
	}
	slices.Sort(ids)
	return ids
}

// tagFilter lists scenes with any of the include tags and none of the
// exclude tags, resolved through the tag cache; a library without any of
// the include tags (or no cache yet) has nothing to list.
func tagFilter(svc *Service, include, exclude []string) (gql.SceneFilterType, bool) {
	ids := svc.tagIDsNamed(include)
	if len(ids) == 0 {
		return gql.SceneFilterType{}, false
	}
	return gql.SceneFilterType{Tags: &gql.HierarchicalMultiCriterionInput{
		Modifier: gql.CriterionModifierIncludes,
		Value:    ids,
		Excludes: svc.tagIDsNamed(exclude),
	}}, true
}

func topQualityFilter(svc *Service, _ time.Time) (gql.SceneFilterType, bool) {
	return tagFilter(svc, topQualityTags, topQualityExclude)
}

func passthroughFilter(svc *Service, _ time.Time) (gql.SceneFilterType, bool) {
	return tagFilter(svc, passthroughTags, nil)
}

// smartSections lists the computed sections in their default display order.
func smartSections() []SmartSection {
	return []SmartSection{
		{Key: "continue", Name: "Continue watching", Default: true, Playa: true, sort: "last_played_at",
			filter: gql.SceneFilterType{Resume_time: intCriterion(0, gql.CriterionModifierGreaterThan)}},
		{Key: "recommended", Name: "Recommended for you", Default: true, Playa: true, After: "continue",
			ids: (*Service).recommendedIDs},
		{Key: "recent", Name: "Recently added", Default: true, Playa: true, sort: "created_at"},
		{Key: "unwatched", Name: "Unwatched", Playa: true, sort: "created_at",
			filter: gql.SceneFilterType{Play_count: intCriterion(0, gql.CriterionModifierEquals)}},
		{Key: "toprated", Name: "Top rated", Playa: true, sort: "rating",
			filter: gql.SceneFilterType{Rating100: intCriterion(0, gql.CriterionModifierGreaterThan)}},
		{Key: "random", Name: "Random", Default: true, sort: "random"},
		{Key: "withscript", Name: "With script", Playa: true, sort: "created_at",
			filter: gql.SceneFilterType{Interactive: boolPtr(true)}},
		{Key: "noscript", Name: "Without script", Playa: true, sort: "created_at",
			filter: gql.SceneFilterType{Interactive: boolPtr(false)}},
		{Key: "favperf", Name: "From your favourite performers", Playa: true, After: "noscript", sort: "created_at",
			filter: gql.SceneFilterType{Performer_favorite: boolPtr(true)}},
		// Rated 80 or more and played at most once: Stash has no >= or <=
		// modifier, so the bounds are shifted by one.
		{Key: "gems", Name: "Hidden gems", Playa: true, After: "favperf", sort: "rating",
			filter: gql.SceneFilterType{Rating100: intCriterion(79, gql.CriterionModifierGreaterThan), Play_count: intCriterion(2, gql.CriterionModifierLessThan)}},
		{Key: "rewatch", Name: "Worth a rewatch", Playa: true, After: "gems", sort: "o_counter", dynFilter: rewatchFilter},
		{Key: "topquality", Name: "Top quality", Playa: true, After: "rewatch", sort: "created_at", dynFilter: topQualityFilter},
		{Key: "passthrough", Name: "Passthrough", Playa: true, After: "topquality", sort: "created_at", dynFilter: passthroughFilter},
	}
}

// SmartSections returns the definitions, for display.
func SmartSections() []SmartSection { return smartSections() }

// query builds the Stash query for this section with the given size. ok is
// false when the section's dynFilter has nothing to query for right now.
func (s SmartSection) query(svc *Service, size int) (filter *gql.SceneFilterType, opts *gql.FindFilterType, ok bool) {
	f := s.filter
	if s.dynFilter != nil {
		if f, ok = s.dynFilter(svc, svc.now()); !ok {
			return nil, nil, false
		}
	}
	sort := s.sort
	dir := gql.SortDirectionEnumDesc
	return &f, &gql.FindFilterType{Per_page: &size, Sort: &sort, Direction: &dir}, true
}
