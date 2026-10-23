package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Family ids: the rows that carry an auto section family's settings and
// anchor the sections it generates.
const (
	FamilyStudio    = "auto:studio"
	FamilyPerformer = "auto:performer"
	FamilyMoreFrom  = "auto:morefrom"
)

// Defaults for the section settings; a row without the option uses them.
const (
	DefaultGemsMinRating   = 80
	DefaultGemsMaxPlays    = 1
	DefaultRewatchDays     = 30
	DefaultFamilyMinScenes = 1
	DefaultFamilyLimit     = 50
	DefaultMoreFromLimit   = 5
	// MaxFamilyLimit caps how many sections a family may generate, so a
	// low threshold cannot flood the players.
	MaxFamilyLimit = 50
)

// Default tag names for the tag-based sections; vrQualityTags manages the
// tier tags, the passthrough ones are the usual names for an alpha matte
// or chroma key.
var (
	DefaultTopQualityTags    = []string{"8K", "7K"}
	DefaultTopQualityExclude = []string{"Low Detail"}
	DefaultPassthroughTags   = []string{"Alpha", "Chroma Key", "Passthrough"}
)

// SectionOptions is the union of every setting any section accepts; a
// section only reads the fields its schema lists and nil means default.
type SectionOptions struct {
	Size          *int     `json:"size,omitempty"`
	UnwatchedOnly *bool    `json:"unwatched_only,omitempty"`
	MinRating     *int     `json:"min_rating,omitempty"`
	MaxPlays      *int     `json:"max_plays,omitempty"`
	Days          *int     `json:"days,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ExcludeTags   []string `json:"exclude_tags,omitempty"`
	MinScenes     *int     `json:"min_scenes,omitempty"`
	Limit         *int     `json:"limit,omitempty"`
	ExcludeMale   *bool    `json:"exclude_male,omitempty"`
}

// OptionKind is how a setting is entered: a number, a tick, or tag names.
type OptionKind string

const (
	OptionInt  OptionKind = "int"
	OptionBool OptionKind = "bool"
	OptionTags OptionKind = "tags"
)

// Is reports whether the kind is s; for templates, which cannot compare a
// named string type to a literal.
func (k OptionKind) Is(s string) bool { return string(k) == s }

// OptionField describes one setting of a section for validation and for
// the Sections page: Default is shown as the input's placeholder.
type OptionField struct {
	Key     string
	Label   string
	Kind    OptionKind
	Min     int
	Max     int
	Default string
	Help    string
}

var sizeField = OptionField{Key: "size", Label: "Scenes shown", Kind: OptionInt, Min: 10, Max: 500, Help: "Blank uses Scenes per smart section."}

func joinTags(t []string) string { return strings.Join(t, ", ") }

// SectionSchema lists the settings the section id accepts, nil for one
// that takes none (saved filters and generated sections). Every smart
// section takes a size; the families and a few smart sections take more.
func SectionSchema(id string) []OptionField {
	switch id {
	case "smart:favperf":
		return []OptionField{sizeField, {Key: "unwatched_only", Label: "Unwatched only", Kind: OptionBool}}
	case "smart:gems":
		return []OptionField{sizeField,
			{Key: "min_rating", Label: "Rated at least", Kind: OptionInt, Min: 1, Max: 100, Default: strconv.Itoa(DefaultGemsMinRating)},
			{Key: "max_plays", Label: "Played at most", Kind: OptionInt, Min: 0, Max: 1000, Default: strconv.Itoa(DefaultGemsMaxPlays)}}
	case "smart:rewatch":
		return []OptionField{sizeField, {Key: "days", Label: "Days since last played", Kind: OptionInt, Min: 1, Max: 3650, Default: strconv.Itoa(DefaultRewatchDays)}}
	case "smart:topquality":
		return []OptionField{sizeField,
			{Key: "tags", Label: "Any of these tags", Kind: OptionTags, Default: joinTags(DefaultTopQualityTags), Help: "Comma separated."},
			{Key: "exclude_tags", Label: "But none of these", Kind: OptionTags, Default: joinTags(DefaultTopQualityExclude)}}
	case "smart:passthrough":
		return []OptionField{sizeField, {Key: "tags", Label: "Any of these tags", Kind: OptionTags, Default: joinTags(DefaultPassthroughTags), Help: "Comma separated."}}
	case FamilyStudio:
		return []OptionField{
			{Key: "min_scenes", Label: "Only studios with at least N scenes", Kind: OptionInt, Min: 1, Max: MaxAutoSectionMin, Default: strconv.Itoa(DefaultFamilyMinScenes), Help: "A studio with fewer scenes gets no section."},
			{Key: "limit", Label: "At most N sections", Kind: OptionInt, Min: 1, Max: MaxFamilyLimit, Default: strconv.Itoa(DefaultFamilyLimit), Help: "The studios with the most scenes; the rest get none."}}
	case FamilyPerformer:
		return []OptionField{
			{Key: "min_scenes", Label: "Only performers with at least N scenes", Kind: OptionInt, Min: 1, Max: MaxAutoSectionMin, Default: strconv.Itoa(DefaultFamilyMinScenes), Help: "A performer with fewer scenes gets no section."},
			{Key: "limit", Label: "At most N sections", Kind: OptionInt, Min: 1, Max: MaxFamilyLimit, Default: strconv.Itoa(DefaultFamilyLimit), Help: "The performers with the most scenes; the rest get none."},
			{Key: "exclude_male", Label: "Leave out male performers", Kind: OptionBool}}
	case FamilyMoreFrom:
		return []OptionField{
			{Key: "limit", Label: "At most N performers", Kind: OptionInt, Min: 1, Max: MaxFamilyLimit, Default: strconv.Itoa(DefaultMoreFromLimit), Help: "The ones you watch most."},
			sizeField}
	}
	if strings.HasPrefix(id, "smart:") {
		return []OptionField{sizeField}
	}
	return nil
}

// setOption is one field of SectionOptions that is set, with its key.
type setOption struct {
	key  string
	n    *int
	b    *bool
	tags []string
}

// set lists the fields set on o, in schema key order.
func (o *SectionOptions) set() []setOption {
	if o == nil {
		return nil
	}
	var out []setOption
	add := func(key string, n *int, b *bool, tags []string) {
		if n != nil || b != nil || len(tags) > 0 {
			out = append(out, setOption{key: key, n: n, b: b, tags: tags})
		}
	}
	add("size", o.Size, nil, nil)
	add("unwatched_only", nil, o.UnwatchedOnly, nil)
	add("min_rating", o.MinRating, nil, nil)
	add("max_plays", o.MaxPlays, nil, nil)
	add("days", o.Days, nil, nil)
	add("tags", nil, nil, o.Tags)
	add("exclude_tags", nil, nil, o.ExcludeTags)
	add("min_scenes", o.MinScenes, nil, nil)
	add("limit", o.Limit, nil, nil)
	add("exclude_male", nil, o.ExcludeMale, nil)
	return out
}

// Value is the set value of key as the Sections page shows it: the number,
// "true" for a ticked box, tags joined with commas; empty when not set.
func (o *SectionOptions) Value(key string) string {
	for _, s := range o.set() {
		if s.key != key {
			continue
		}
		switch {
		case s.n != nil:
			return strconv.Itoa(*s.n)
		case s.b != nil && *s.b:
			return "true"
		case len(s.tags) > 0:
			return joinTags(s.tags)
		}
	}
	return ""
}

// validateOptions checks row i's options against its section's schema.
func validateOptions(i int, f *Filter) error {
	if f.Options == nil {
		return nil
	}
	schema := SectionSchema(f.ID)
	if schema == nil {
		return fmt.Errorf("%w: filters[%d].options: %s has no settings", ErrInvalid, i, f.ID)
	}
	for _, s := range f.Options.set() {
		at := -1
		for j := range schema {
			if schema[j].Key == s.key {
				at = j
				break
			}
		}
		if at < 0 {
			return fmt.Errorf("%w: filters[%d].options.%s is not a setting of %s", ErrInvalid, i, s.key, f.ID)
		}
		if fld := schema[at]; fld.Kind == OptionInt && (*s.n < fld.Min || *s.n > fld.Max) {
			return fmt.Errorf("%w: filters[%d].options.%s must be between %d and %d, got %d", ErrInvalid, i, s.key, fld.Min, fld.Max, *s.n)
		}
	}
	return nil
}

// cleanTags trims the names and drops blank ones; nil when none remain.
func cleanTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// normalizeFilters cleans tag lists in place and drops an options object
// with nothing set, so a cleared form stores no options.
func normalizeFilters(filters []Filter) {
	for i := range filters {
		o := filters[i].Options
		if o == nil {
			continue
		}
		o.Tags = cleanTags(o.Tags)
		o.ExcludeTags = cleanTags(o.ExcludeTags)
		if len(o.set()) == 0 {
			filters[i].Options = nil
		}
	}
}
