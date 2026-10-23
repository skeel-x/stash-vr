package library

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

func smartByKey(t *testing.T, key string) SmartSection {
	t.Helper()
	for _, s := range smartSections() {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no smart section %q", key)
	return SmartSection{}
}

func TestSmartSection_StaticFilters(t *testing.T) {
	cases := []struct {
		key  string
		sort string
		want gql.SceneFilterType
	}{
		{key: "favperf", sort: "created_at", want: gql.SceneFilterType{Performer_favorite: boolPtr(true)}},
		{key: "gems", sort: "rating", want: gql.SceneFilterType{
			Rating100:  intCriterion(79, gql.CriterionModifierGreaterThan),
			Play_count: intCriterion(2, gql.CriterionModifierLessThan),
		}},
	}
	svc := NewService(&routingStash{})
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s := smartByKey(t, c.key)
			if s.Default || !s.Playa {
				t.Fatalf("%s must be off by default and listed in Playa: %+v", c.key, s)
			}
			filter, opts, ok := s.query(svc, 7)
			if !ok {
				t.Fatal("a static filter is always applicable")
			}
			got, _ := json.Marshal(filter)
			want, _ := json.Marshal(c.want)
			if string(got) != string(want) {
				t.Fatalf("filter = %s, want %s", got, want)
			}
			if got := fmt.Sprintf("%v %s %s", *opts.Per_page, *opts.Sort, *opts.Direction); got != "7 "+c.sort+" DESC" {
				t.Fatalf("opts = %s", got)
			}
		})
	}
}

func TestRewatchFilter_ThresholdMovesWithTheClock(t *testing.T) {
	cases := []struct {
		now  time.Time
		want string
	}{
		{now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), want: "2026-08-02 12:00:00"},
		{now: time.Date(2026, 1, 15, 0, 30, 0, 0, time.FixedZone("CET", 3600)), want: "2025-12-15 23:30:00"},
	}
	for _, c := range cases {
		filter, ok := rewatchFilter(nil, c.now)
		if !ok {
			t.Fatal("rewatch is always applicable")
		}
		if lp := filter.Last_played_at; lp == nil || lp.Value != c.want || lp.Modifier != gql.CriterionModifierLessThan {
			t.Errorf("now %v: last_played_at = %+v, want < %s", c.now, lp, c.want)
		}
		if oc := filter.O_counter; oc == nil || oc.Value != 0 || oc.Modifier != gql.CriterionModifierGreaterThan {
			t.Errorf("o_counter = %+v, want > 0", oc)
		}
	}
}

func TestGetSections_RewatchQueriesWithTheServiceClock(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: "smart:rewatch"}})
	stash := &routingStash{}
	now := recNow
	svc := recService(stash, &now)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if sectionByID(sections, "smart:rewatch") == nil {
		t.Fatalf("expected the rewatch section, got %v", names(sections))
	}
	q := stash.queryWithSort("o_counter")
	if q == nil || q.Scene_filter == nil || q.Scene_filter.Last_played_at == nil {
		t.Fatalf("expected an o_counter query with a last_played_at criterion, got %+v", q)
	}
	if got := q.Scene_filter.Last_played_at.Value; got != "2026-08-02 12:00:00" {
		t.Fatalf("threshold = %s, want 30 days before the clock", got)
	}
}

func TestGetSections_DynFilterSkipsSectionQuietly(t *testing.T) {
	loadConfig(t, nil)
	svc := NewService(&routingStash{})
	s := SmartSection{Key: "never", Name: "Never", sort: "created_at",
		dynFilter: func(*Service, time.Time) (gql.SceneFilterType, bool) { return gql.SceneFilterType{}, false }}

	if _, _, ok := s.query(svc, 10); ok {
		t.Fatal("expected the section to opt out")
	}
	sets, err := svc.resolveSections(context.Background(), []sectionSource{
		{row: SectionRow{ID: s.ID(), Name: s.Name}, smart: &s},
		{row: SectionRow{ID: "smart:recent", Name: "Recently added"}, smart: ptr(smartByKey(t, "recent"))},
	})
	if err != nil {
		t.Fatalf("a skipped section is not a failure: %v", err)
	}
	if len(sets) != 1 || sets[0].ID != "smart:recent" {
		t.Fatalf("expected only the recent section, got %+v", sets)
	}
}

func ptr[T any](v T) *T { return &v }
