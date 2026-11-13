package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func intp(v int) *int    { return &v }
func boolp(v bool) *bool { return &v }

func TestValidateFilters_AcceptsOptionsTheSectionLists(t *testing.T) {
	f := []Filter{
		{ID: "smart:gems", Options: &SectionOptions{MinRating: intp(90), MaxPlays: intp(0), Size: intp(20)}},
		{ID: "smart:rewatch", Options: &SectionOptions{Days: intp(90)}},
		{ID: "smart:favperf", Options: &SectionOptions{UnwatchedOnly: boolp(true)}},
		{ID: "smart:topquality", Options: &SectionOptions{Tags: []string{"8K"}, ExcludeTags: []string{"Low Detail"}}},
		{ID: "smart:recent", Options: &SectionOptions{Size: intp(10)}},
		{ID: FamilyStudio, Options: &SectionOptions{MinScenes: intp(20), Limit: intp(10)}},
		{ID: FamilyMoreFrom, Options: &SectionOptions{Limit: intp(3), Size: intp(25)}},
	}
	if err := validateFilters(f); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFilters_RejectsOptionsOutsideTheSchema(t *testing.T) {
	cases := []struct {
		name string
		f    Filter
		want string
	}{
		{"saved filter takes none", Filter{ID: "12", Options: &SectionOptions{Size: intp(20)}}, "filters[0].options: 12 has no settings"},
		{"wrong key", Filter{ID: "smart:gems", Options: &SectionOptions{Days: intp(5)}}, "filters[0].options.days is not a setting of smart:gems"},
		{"size below", Filter{ID: "smart:recent", Options: &SectionOptions{Size: intp(9)}}, "filters[0].options.size must be between 10 and 500"},
		{"rating above", Filter{ID: "smart:gems", Options: &SectionOptions{MinRating: intp(101)}}, "filters[0].options.min_rating must be between 1 and 100"},
		{"limit above", Filter{ID: FamilyPerformer, Options: &SectionOptions{Limit: intp(51)}}, "filters[0].options.limit must be between 1 and 50"},
		{"tags on family", Filter{ID: FamilyStudio, Options: &SectionOptions{Tags: []string{"8K"}}}, "filters[0].options.tags is not a setting of auto:studio"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateFilters([]Filter{c.f})
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrInvalid containing %q", err, c.want)
			}
		})
	}
}

func TestNormalizeFilters_DropsBlankTagsAndEmptyOptions(t *testing.T) {
	f := []Filter{
		{ID: "smart:topquality", Options: &SectionOptions{Tags: []string{"  8K ", "", "   "}}},
		{ID: "smart:passthrough", Options: &SectionOptions{Tags: []string{"", " "}}},
		{ID: "smart:gems", Options: &SectionOptions{}},
	}
	normalizeFilters(f)
	if got := f[0].Options.Tags; len(got) != 1 || got[0] != "8K" {
		t.Fatalf("tags = %q, want [8K]", got)
	}
	if f[1].Options != nil {
		t.Fatalf("blank tags must leave no options, got %+v", f[1].Options)
	}
	if f[2].Options != nil {
		t.Fatalf("empty options must become nil, got %+v", f[2].Options)
	}
}

func TestSectionSchema_SmartSectionsAlwaysHaveSize(t *testing.T) {
	s := SectionSchema("smart:continue")
	if len(s) != 1 || s[0].Key != "size" || s[0].Kind != OptionInt || s[0].Min != 10 || s[0].Max != 500 {
		t.Fatalf("schema = %+v", s)
	}
	if SectionSchema("12") != nil || SectionSchema("studio:4") != nil {
		t.Fatal("saved filters and auto children have no schema")
	}
}

func TestSectionOptions_ValueFormatsSetFields(t *testing.T) {
	o := &SectionOptions{Days: intp(45), UnwatchedOnly: boolp(true), Tags: []string{"8K", "7K"}}
	for key, want := range map[string]string{"days": "45", "unwatched_only": "true", "tags": "8K, 7K", "size": ""} {
		if got := o.Value(key); got != want {
			t.Fatalf("Value(%s) = %q, want %q", key, got, want)
		}
	}
	var nilOpts *SectionOptions
	if nilOpts.Value("days") != "" {
		t.Fatal("nil options have no values")
	}
}

func TestCloneConfig_CopiesOptionTagLists(t *testing.T) {
	c := ApplicationConfig{Filters: []Filter{
		{ID: "smart:topquality", Options: &SectionOptions{Tags: []string{"8K"}}},
		{ID: FamilyPerformer, Options: &SectionOptions{Performers: []string{"72"}}},
	}}
	out := cloneConfig(c)
	out.Filters[0].Options.Tags[0] = "7K"
	if c.Filters[0].Options.Tags[0] != "8K" {
		t.Fatal("clone must not share the tag list")
	}
	out.Filters[1].Options.Performers[0] = "1"
	if c.Filters[1].Options.Performers[0] != "72" {
		t.Fatal("clone must not share the performer picks")
	}
}

func TestSectionSchema_ExcludeMaleOnlyForPerformerGroup(t *testing.T) {
	if err := validateFilters([]Filter{{ID: FamilyPerformer, Options: &SectionOptions{ExcludeMale: boolp(true)}}}); err != nil {
		t.Fatal(err)
	}
	err := validateFilters([]Filter{{ID: FamilyStudio, Options: &SectionOptions{ExcludeMale: boolp(true)}}})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "options.exclude_male is not a setting of auto:studio") {
		t.Fatalf("err = %v", err)
	}
	if (&SectionOptions{ExcludeMale: boolp(true)}).Value("exclude_male") != "true" {
		t.Fatal("Value must report the tick")
	}
}

func TestValidateFilters_PerformerPicks(t *testing.T) {
	ok := []Filter{{ID: FamilyPerformer, Options: &SectionOptions{Performers: []string{"72", "515"}}}}
	if err := validateFilters(ok); err != nil {
		t.Fatal(err)
	}
	many := make([]string, MaxPickedPerformers+1)
	for i := range many {
		many[i] = strconv.Itoa(i + 1)
	}
	cases := []struct {
		name string
		f    Filter
		want string
	}{
		{"not on studio", Filter{ID: FamilyStudio, Options: &SectionOptions{Performers: []string{"1"}}}, "filters[0].options.performers is not a setting of auto:studio"},
		{"not on smart", Filter{ID: "smart:favperf", Options: &SectionOptions{Performers: []string{"1"}}}, "filters[0].options.performers is not a setting of smart:favperf"},
		{"too many", Filter{ID: FamilyPerformer, Options: &SectionOptions{Performers: many}}, "filters[0].options.performers holds 51 performers, at most 50"},
		{"blank id", Filter{ID: FamilyPerformer, Options: &SectionOptions{Performers: []string{"1", " "}}}, "filters[0].options.performers[1] is blank"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateFilters([]Filter{c.f})
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrInvalid containing %q", err, c.want)
			}
		})
	}
}

func TestNormalizeFilters_CleansPerformerPicks(t *testing.T) {
	f := []Filter{
		{ID: FamilyPerformer, Options: &SectionOptions{Performers: []string{" 72 ", "", "515", "72"}}},
		{ID: FamilyPerformer, Options: &SectionOptions{Performers: []string{" ", ""}}},
	}
	normalizeFilters(f)
	if got := f[0].Options.Performers; fmt.Sprint(got) != "[72 515]" {
		t.Fatalf("performers = %v, want [72 515]", got)
	}
	if f[1].Options != nil {
		t.Fatalf("blank picks must store no options, got %+v", f[1].Options)
	}
	if v := f[0].Options.Value("performers"); v != "72, 515" {
		t.Fatalf("Value = %q", v)
	}
}

func TestSectionSchema_PerformerFamilyListsThePicker(t *testing.T) {
	s := SectionSchema(FamilyPerformer)
	last := s[len(s)-1]
	if last.Key != "performers" || last.Kind != OptionPerformers || last.Label != "Only these starred performers" {
		t.Fatalf("last field = %+v", last)
	}
	for _, id := range []string{FamilyStudio, FamilyMoreFrom, "smart:favperf"} {
		for _, f := range SectionSchema(id) {
			if f.Key == "performers" {
				t.Fatalf("%s must not list performers", id)
			}
		}
	}
}
