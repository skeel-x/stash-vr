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

// withTags fills the service's tag cache with {id, name} pairs.
func withTags(svc *Service, tags ...[2]string) {
	cache := make(map[string]*Tag, len(tags))
	for _, t := range tags {
		cache[t[0]] = &Tag{Id: t[0], Name: t[1], SortName: t[1]}
	}
	svc.muTagCache.Lock()
	svc.tagCache = cache
	svc.muTagCache.Unlock()
}

func TestTagIDsNamed(t *testing.T) {
	svc := NewService(&routingStash{})
	if got := svc.tagIDsNamed([]string{"8K"}); got != nil {
		t.Fatalf("no cache must give nil, got %v", got)
	}
	withTags(svc, [2]string{"9", "8k"}, [2]string{"10", "7K"}, [2]string{"3", "Low detail"}, [2]string{"4", "8K UHD"})
	if got := fmt.Sprint(svc.tagIDsNamed([]string{"8K", "7K"})); got != "[10 9]" {
		t.Fatalf("expected case-insensitive whole-name matches in id order, got %v", got)
	}
	if got := svc.tagIDsNamed([]string{"Alpha"}); got == nil || len(got) != 0 {
		t.Fatalf("a loaded cache without the tag gives an empty, non-nil list, got %#v", got)
	}
}

func TestTagSections_ResolveTagIDsOrSkip(t *testing.T) {
	cases := []struct {
		name         string
		key          string
		tags         [][2]string
		wantInclude  string
		wantExcludes string
		skip         bool
	}{
		{name: "no cache", key: "topquality", skip: true},
		{name: "no tier tag", key: "topquality", tags: [][2]string{{"1", "Low Detail"}, {"2", "DOME"}}, skip: true},
		{name: "tier tags with exclusion", key: "topquality", tags: [][2]string{{"1", "Low Detail"}, {"2", "8K"}, {"3", "7k"}}, wantInclude: "[2 3]", wantExcludes: "[1]"},
		{name: "tier tags without exclusion tag", key: "topquality", tags: [][2]string{{"2", "8K"}}, wantInclude: "[2]", wantExcludes: "[]"},
		{name: "no passthrough tag", key: "passthrough", tags: [][2]string{{"2", "8K"}}, skip: true},
		{name: "passthrough tags", key: "passthrough", tags: [][2]string{{"5", "alpha"}, {"6", "Chroma Key"}, {"7", "Passthrough"}, {"8", "Alphabet"}}, wantInclude: "[5 6 7]", wantExcludes: "[]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := NewService(&routingStash{})
			if c.tags != nil {
				withTags(svc, c.tags...)
			}
			filter, opts, ok := smartByKey(t, c.key).query(svc, 5)
			if ok == c.skip {
				t.Fatalf("ok = %v, want %v", ok, !c.skip)
			}
			if c.skip {
				return
			}
			tags := filter.Tags
			if tags == nil || tags.Modifier != gql.CriterionModifierIncludes {
				t.Fatalf("expected an INCLUDES tag criterion, got %+v", tags)
			}
			if got := fmt.Sprint(tags.Value); got != c.wantInclude {
				t.Errorf("value = %s, want %s", got, c.wantInclude)
			}
			if got := fmt.Sprint(tags.Excludes); got != c.wantExcludes {
				t.Errorf("excludes = %s, want %s", got, c.wantExcludes)
			}
			if *opts.Sort != "created_at" {
				t.Errorf("sort = %s, want created_at", *opts.Sort)
			}
		})
	}
}

func TestGetSections_TagSectionsUseTheTagsLoadedForTheBuild(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: "smart:topquality"}, {ID: "smart:passthrough"}})
	stash := &routingStash{tags: `[{"id":"21","name":"8K"},{"id":"22","name":"Low Detail"}]`}
	svc := NewService(stash)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if sectionByID(sections, "smart:topquality") == nil {
		t.Fatalf("expected Top quality on the first build, got %v", names(sections))
	}
	if sectionByID(sections, "smart:passthrough") != nil {
		t.Fatalf("no passthrough tag exists, got %v", names(sections))
	}
	var q *sceneIdQuery
	for _, cand := range stash.queries {
		if cand.Scene_filter != nil && cand.Scene_filter.Tags != nil {
			q = cand
		}
	}
	if q == nil || fmt.Sprint(q.Scene_filter.Tags.Value) != "[21]" || fmt.Sprint(q.Scene_filter.Tags.Excludes) != "[22]" {
		t.Fatalf("expected one tag query including 21 and excluding 22, got %+v", q)
	}
}
