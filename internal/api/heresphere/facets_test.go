package heresphere

import (
	"strings"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestAgeAt(t *testing.T) {
	at := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		birth string
		want  int
		ok    bool
	}{
		{"1990-06-15", 34, true}, {"1990-06-16", 33, true}, {"1990-01-01", 34, true}, {"", 0, false}, {"not a date", 0, false},
		// A birthday later in the year is not reached yet, whatever the leap
		// day between the two dates does to the day-of-year.
		{"1990-12-31", 33, true}, {"2030-01-01", 0, false},
	}
	for _, c := range cases {
		got, ok := ageAt(c.birth, at)
		if got != c.want || ok != c.ok {
			t.Errorf("ageAt(%q) = %d,%v want %d,%v", c.birth, got, ok, c.want, c.ok)
		}
	}
}

func facetPerformer(name, country, birth string) *gql.ScenePartsPerformersPerformer {
	p := &gql.ScenePartsPerformersPerformer{Id: name, Name: name}
	if country != "" {
		p.Country = util.Ptr(country)
	}
	if birth != "" {
		p.Birthdate = util.Ptr(birth)
	}
	return p
}

func TestPerformerFacets_UsesSceneDateThenToday(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	dated := &library.VideoData{SceneParts: &gql.SceneParts{Id: "1", Date: util.Ptr("2020-01-01"),
		Performers: []*gql.ScenePartsPerformersPerformer{facetPerformer("A", "Sweden", "1990-06-15"), facetPerformer("B", "", ""), facetPerformer("C", "Sweden", "")}}}
	got := names(performerFacets(dated, now))
	want := []string{"Country:Sweden", "Age:29"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("dated: got %v want %v", got, want)
	}

	undated := &library.VideoData{SceneParts: &gql.SceneParts{Id: "2",
		Performers: []*gql.ScenePartsPerformersPerformer{facetPerformer("A", "", "1990-06-15")}}}
	if got := names(performerFacets(undated, now)); len(got) != 1 || got[0] != "Age:36" {
		t.Fatalf("undated: got %v", got)
	}

	// Two performers of different ages give two Age tags; the same age
	// twice gives one, and a nil performer is skipped.
	mixed := &library.VideoData{SceneParts: &gql.SceneParts{Id: "3", Date: util.Ptr("2020-01-01"),
		Performers: []*gql.ScenePartsPerformersPerformer{facetPerformer("A", "", "1990-06-15"), nil, facetPerformer("B", "", "1990-07-01"), facetPerformer("C", "", "1985-01-01")}}}
	if got := names(performerFacets(mixed, now)); len(got) != 2 || got[0] != "Age:29" || got[1] != "Age:35" {
		t.Fatalf("mixed: got %v", got)
	}
}

func TestGetTags_PerformerFacetsFollowSetting(t *testing.T) {
	loadDefaultRules(t)
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "9",
		Files:      []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
		Performers: []*gql.ScenePartsPerformersPerformer{facetPerformer("A", "Sweden", "1990-06-15")}}}
	hasPrefix := func(tags []tagDto, prefix string) bool {
		for _, tag := range tags {
			if strings.HasPrefix(tag.Name, prefix) {
				return true
			}
		}
		return false
	}

	cfg := config.Application()
	cfg.PerformerFacets = false
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if tags := getTags(vd); hasPrefix(tags, "Country:") || hasPrefix(tags, "Age:") {
		t.Fatalf("expected no facets with the setting off, got %v", names(tags))
	}

	cfg.PerformerFacets = true
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	tags := getTags(vd)
	if !hasPrefix(tags, "Country:Sweden") || !hasPrefix(tags, "Age:") || !hasPrefix(tags, "@:A") {
		t.Fatalf("expected Country, Age and performer tags with the setting on, got %v", names(tags))
	}
	for _, tag := range tags {
		if strings.HasPrefix(tag.Name, "Country:") && (tag.Track == nil || tag.Start != 0 || tag.End != nil) {
			t.Fatalf("facet should sit on its own full-length track, got %+v", tag)
		}
	}
}

func names(tags []tagDto) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.Name
	}
	return out
}
