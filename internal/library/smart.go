package library

import "stash-vr/internal/stash/gql"

// SmartSection is a computed section: a fixed Stash query instead of a saved
// filter. Users switch, order and rename them like saved filters through
// config.Filter entries whose ID is "smart:<key>".
type SmartSection struct {
	Key     string
	Name    string
	Default bool // enabled when no override entry exists for it
	// Playa lists this section as a category; Random is left out because
	// Playa has its own randomiser.
	Playa  bool
	filter gql.SceneFilterType
	sort   string
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

// smartSections lists the computed sections in their default display order.
func smartSections() []SmartSection {
	return []SmartSection{
		{Key: "continue", Name: "Continue watching", Default: true, Playa: true, sort: "last_played_at",
			filter: gql.SceneFilterType{Resume_time: intCriterion(0, gql.CriterionModifierGreaterThan)}},
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
	}
}

// SmartSections returns the definitions, for display.
func SmartSections() []SmartSection { return smartSections() }

// query builds the Stash query for this section with the given size.
func (s SmartSection) query(size int) (*gql.SceneFilterType, *gql.FindFilterType) {
	f := s.filter
	sort := s.sort
	dir := gql.SortDirectionEnumDesc
	return &f, &gql.FindFilterType{Per_page: &size, Sort: &sort, Direction: &dir}
}
