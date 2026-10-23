package library

import (
	"fmt"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

func boolp(v bool) *bool { return &v }

func TestGemsFilter_DefaultsAndOptions(t *testing.T) {
	loadConfig(t, nil)
	f, _ := gemsFilter(nil, time.Time{})
	if f.Rating100.Value != 79 || f.Rating100.Modifier != gql.CriterionModifierGreaterThan || f.Play_count.Value != 2 || f.Play_count.Modifier != gql.CriterionModifierLessThan {
		t.Fatalf("defaults: %+v %+v", f.Rating100, f.Play_count)
	}
	loadConfig(t, []config.Filter{{ID: "smart:gems", Options: &config.SectionOptions{MinRating: intp(90), MaxPlays: intp(0)}}})
	f, _ = gemsFilter(nil, time.Time{})
	if f.Rating100.Value != 89 || f.Play_count.Value != 1 {
		t.Fatalf("options: %+v %+v", f.Rating100, f.Play_count)
	}
}

func TestRewatchFilter_UsesDays(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: "smart:rewatch", Options: &config.SectionOptions{Days: intp(10)}}})
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	f, _ := rewatchFilter(nil, now)
	if f.Last_played_at.Value != "2026-10-13 12:00:00" {
		t.Fatalf("last_played_at = %q", f.Last_played_at.Value)
	}
}

func TestFavPerfFilter_UnwatchedOnly(t *testing.T) {
	loadConfig(t, nil)
	f, _ := favPerfFilter(nil, time.Time{})
	if f.Play_count != nil || f.Performer_favorite == nil || !*f.Performer_favorite {
		t.Fatalf("default: %+v", f)
	}
	loadConfig(t, []config.Filter{{ID: "smart:favperf", Options: &config.SectionOptions{UnwatchedOnly: boolp(true)}}})
	f, _ = favPerfFilter(nil, time.Time{})
	if f.Play_count == nil || f.Play_count.Value != 0 || f.Play_count.Modifier != gql.CriterionModifierEquals || !*f.Performer_favorite {
		t.Fatalf("unwatched only: %+v", f)
	}
}

func TestTagSections_UseOptionTags(t *testing.T) {
	loadConfig(t, []config.Filter{
		{ID: "smart:passthrough", Options: &config.SectionOptions{Tags: []string{"Chroma Key"}}},
		{ID: "smart:topquality", Options: &config.SectionOptions{ExcludeTags: []string{"Alpha"}}},
	})
	svc := NewService(&routingStash{})
	withTags(svc, [2]string{"1", "8K"}, [2]string{"2", "Alpha"}, [2]string{"3", "Chroma Key"}, [2]string{"4", "Low Detail"})
	f, ok := passthroughFilter(svc, time.Time{})
	if !ok || fmt.Sprint(f.Tags.Value) != "[3]" {
		t.Fatalf("passthrough = %+v ok=%v", f.Tags, ok)
	}
	// Top quality keeps the default include tags and takes the exclusion.
	f, ok = topQualityFilter(svc, time.Time{})
	if !ok || fmt.Sprint(f.Tags.Value) != "[1]" || fmt.Sprint(f.Tags.Excludes) != "[2]" {
		t.Fatalf("top quality = %+v ok=%v", f.Tags, ok)
	}
}

func TestOptionHelpers(t *testing.T) {
	if boolOr(nil, true) != true || boolOr(boolp(false), true) != false {
		t.Fatal("boolOr")
	}
	if got := tagsOr(nil, []string{"a"}); fmt.Sprint(got) != "[a]" {
		t.Fatalf("tagsOr default = %v", got)
	}
	if got := tagsOr([]string{"b"}, []string{"a"}); fmt.Sprint(got) != "[b]" {
		t.Fatalf("tagsOr set = %v", got)
	}
}
