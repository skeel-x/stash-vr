package library

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// cast adds named performers; gender "" leaves it unknown.
func cast(members ...[3]string) recOpt {
	return func(s *recScene) {
		for _, m := range members {
			p := &recPerformer{Id: m[0], Name: m[1]}
			if m[2] != "" {
				g := gql.GenderEnum(m[2])
				p.Gender = &g
			}
			s.Performers = append(s.Performers, p)
		}
	}
}

var (
	alice = [3]string{"p1", "Alice", "FEMALE"}
	bob   = [3]string{"p2", "Bob", "MALE"}
	cara  = [3]string{"p3", "Cara", ""}
	dee   = [3]string{"p4", "Dee", "NON_BINARY"}
)

func sectionIDs(sections []AutoSection) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.ID
	}
	return out
}

func TestMoreFromSections(t *testing.T) {
	// Newest first, as the query returns them. Scene 1 was watched with
	// Alice, Bob and Cara; 2 with Dee; 3 to 6 are unplayed.
	library := []*recScene{
		rs("6", cast(cara)),
		rs("5", cast(alice, alice), rated(90)), // listed twice: once in her section
		rs("4", cast(alice, bob)),
		rs("3", cast(alice), rated(70)),
		rs("2", cast(dee), watched(2)),
		rs("1", cast(alice, bob, cara), watched(1)),
	}
	cases := []struct {
		name   string
		scenes []*recScene
		n      int
		size   int
		want   []string
	}{
		{name: "off", scenes: library, n: 0, size: 50, want: nil},
		{name: "no history", scenes: []*recScene{rs("4", cast(alice, bob)), rs("3", cast(alice))}, n: 5, size: 50, want: nil},
		{name: "ranked by history, male excluded, unknown gender kept", scenes: library, n: 5, size: 50,
			want: []string{"auto:morefrom:p1", "auto:morefrom:p3"}},
		{name: "limit honoured", scenes: library, n: 1, size: 50, want: []string{"auto:morefrom:p1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := moreFromSections(c.scenes, recNow, c.n, c.size)
			if fmt.Sprint(sectionIDs(got)) != fmt.Sprint(c.want) {
				t.Fatalf("sections = %v, want %v", sectionIDs(got), c.want)
			}
		})
	}

	got := moreFromSections(library, recNow, 5, 50)
	if got[0].Name != "More from Alice" || got[0].After != "recommended" || got[0].Count != 3 {
		t.Fatalf("Alice section wrong: %+v", got[0])
	}
	// Best rated first, unrated last, newest first among equals; scene 1
	// is played and left out.
	if fmt.Sprint(got[0].Ids) != "[5 3 4]" {
		t.Fatalf("Alice ids = %v, want [5 3 4]", got[0].Ids)
	}
	if got[1].Name != "More from Cara" || fmt.Sprint(got[1].Ids) != "[6]" {
		t.Fatalf("Cara section wrong: %+v", got[1])
	}
}

func TestMoreFromSections_DeeHasNothingUnplayedAndTakesNoSlot(t *testing.T) {
	// Dee's scene was watched most recently and scores highest, but she
	// has nothing unplayed; Alice still gets the only slot.
	scenes := []*recScene{
		rs("3", cast(alice)),
		rs("2", cast(dee), watched(0), ocount(3)),
		rs("1", cast(alice), watched(10)),
	}
	got := moreFromSections(scenes, recNow, 1, 50)
	if fmt.Sprint(sectionIDs(got)) != "[auto:morefrom:p1]" {
		t.Fatalf("sections = %v", sectionIDs(got))
	}
}

func TestMoreFromSections_CapsAtSizeAndSkipsNilScenes(t *testing.T) {
	scenes := []*recScene{nil, rs("1", cast(alice), watched(1))}
	for i := 2; i <= 12; i++ {
		scenes = append(scenes, rs(fmt.Sprint(i), cast(alice), rated(i)))
	}
	got := moreFromSections(scenes, recNow, 3, 4)
	if len(got) != 1 || fmt.Sprint(got[0].Ids) != "[12 11 10 9]" || got[0].Count != 4 {
		t.Fatalf("expected the four best rated, got %+v", got)
	}
}

func TestMoreFromSections_OnlyPlayedHistoryCounts(t *testing.T) {
	// Cara's scene is highly rated but never played: it is not watch
	// history for ranking performers.
	scenes := []*recScene{
		rs("4", cast(cara)),
		rs("3", cast(alice)),
		rs("2", cast(cara), rated(95)),
		rs("1", cast(alice), watched(1)),
	}
	got := moreFromSections(scenes, recNow, 5, 50)
	if fmt.Sprint(sectionIDs(got)) != "[auto:morefrom:p1]" {
		t.Fatalf("sections = %v, want only Alice", sectionIDs(got))
	}
}

func loadMoreFromConfig(t *testing.T, n int, filters []config.Filter) {
	t.Helper()
	err := config.Load(config.ApplicationConfig{
		ListenAddress:      ":9666",
		StashGraphQLUrl:    "http://stash:9999/graphql",
		FavoriteTag:        "FAVORITE",
		LogLevel:           "info",
		ExcludeSortName:    "hidden",
		SmartSectionSize:   50,
		ConfigPath:         t.TempDir(),
		Filters:            filters,
		MoreFromPerformers: n,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// moreFromJSON is a library where scene 1 was watched yesterday with Alice
// and Bob; 2 and 3 are unplayed scenes of Alice, 4 an unplayed one of Bob.
func moreFromJSON() string {
	yesterday := recNow.Add(-24 * time.Hour).Format(time.RFC3339)
	file := `"files":[{"width":1920,"height":1080}]`
	return `[
		{"id":"4","tags":[],"performers":[{"id":"p2","name":"Bob","gender":"MALE"}],` + file + `},
		{"id":"3","tags":[],"performers":[{"id":"p1","name":"Alice","gender":"FEMALE"}],"rating100":80,` + file + `},
		{"id":"2","tags":[],"performers":[{"id":"p1","name":"Alice","gender":"FEMALE"}],` + file + `},
		{"id":"1","tags":[],"performers":[{"id":"p1","name":"Alice","gender":"FEMALE"},{"id":"p2","name":"Bob","gender":"MALE"}],"play_count":1,"last_played_at":"` + yesterday + `",` + file + `}
	]`
}

func TestGetSections_MoreFromFollowsRecommendedAndIsCached(t *testing.T) {
	loadMoreFromConfig(t, 3, nil)
	stash := &routingStash{recScenes: moreFromJSON()}
	now := recNow
	svc := recService(stash, &now)
	ctx := context.Background()

	sections, err := svc.GetSections(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, len(sections))
	for i, s := range sections {
		ids[i] = s.ID
	}
	rec := slices.Index(ids, "smart:recommended")
	more := slices.Index(ids, "auto:morefrom:p1")
	if rec < 0 || more != rec+1 {
		t.Fatalf("expected More from Alice right after Recommended, got %v", ids)
	}
	if slices.Contains(ids, "auto:morefrom:p2") {
		t.Fatalf("Bob is male and gets no section, got %v", ids)
	}
	if s := sections[more]; s.Name != "More from Alice" || fmt.Sprint(s.Ids) != "[3 2]" {
		t.Fatalf("More from Alice wrong: %+v", s)
	}
	// Recommended and More from each fetch the library once.
	if n := stash.recQueries(); n != 2 {
		t.Fatalf("expected 2 recommendation queries, got %d", n)
	}

	rows, err := svc.SectionRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	at := slices.IndexFunc(rows, func(r SectionRow) bool { return r.ID == "auto:morefrom:p1" })
	if at < 0 || !rows[at].Auto || rows[at].Disabled || rows[at].SourceName != "More from Alice" || rows[at-1].ID != "smart:recommended" {
		t.Fatalf("Sections page row wrong: %v", rows)
	}
	if n := svc.AutoSectionCount(); n != 1 {
		t.Fatalf("expected the section counted as auto, got %d", n)
	}

	// The index is rebuilt every 60 s; the ranking is not.
	now = now.Add(20 * time.Minute)
	svc.ResetSections()
	if _, err := svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if n := stash.recQueries(); n != 2 {
		t.Fatalf("expected the ranking cached across index rebuilds, got %d queries", n)
	}
	now = now.Add(11 * time.Minute)
	svc.ResetSections()
	if _, err := svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if n := stash.recQueries(); n != 4 {
		t.Fatalf("expected both recomputed after 30 minutes, got %d queries", n)
	}
}

func TestGetSections_MoreFromOffCostsNoQuery(t *testing.T) {
	loadMoreFromConfig(t, 0, []config.Filter{{ID: "smart:recommended", Disabled: true}})
	stash := &routingStash{recScenes: moreFromJSON()}
	svc := NewService(stash)

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SectionRows(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := stash.recQueries(); n != 0 {
		t.Fatalf("expected no recommendation query with the feature off, got %d", n)
	}
}

func TestGetSections_MoreFromHonoursOverrides(t *testing.T) {
	loadMoreFromConfig(t, 3, []config.Filter{
		{ID: "smart:recommended", Disabled: true},
		{ID: "auto:morefrom:p1", Name: "Alice again", HiddenIn: []string{"playa"}},
	})
	now := recNow
	svc := recService(&routingStash{recScenes: moreFromJSON()}, &now)
	ctx := context.Background()

	sections, err := svc.GetSectionsFor(ctx, "heresphere")
	if err != nil {
		t.Fatal(err)
	}
	s := sectionByID(sections, "auto:morefrom:p1")
	if s == nil || s.Name != "Alice again" {
		t.Fatalf("expected the renamed section for HereSphere, got %v", names(sections))
	}
	sets, err := svc.GetSavedFilterSceneSetsFor(ctx, "playa")
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range sets {
		if set.ID == "auto:morefrom:p1" {
			t.Fatal("the section is hidden in Playa")
		}
	}

	loadMoreFromConfig(t, 3, []config.Filter{{ID: "auto:morefrom:p1", Disabled: true}})
	svc.ResetSections()
	sections, err = svc.GetSections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sectionByID(sections, "auto:morefrom:p1") != nil {
		t.Fatalf("a disabled override leaves the section out, got %v", names(sections))
	}
}

func TestGetSections_MoreFromQueryErrorKeepsOtherAutoSections(t *testing.T) {
	loadMoreFromConfig(t, 3, nil)
	cfg := config.Application()
	cfg.AutoStudioMin = 2
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	stash := &routingStash{groupings: twoStudioGroupings, recErr: fmt.Errorf("boom")}
	svc := NewService(stash)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sectionByID(sections, "studio:s1") == nil {
		t.Fatalf("the studio section survives a failed ranking, got %v", names(sections))
	}
	for _, s := range sections {
		if strings.HasPrefix(s.ID, moreFromPrefix) {
			t.Fatalf("no More from section on a failed query, got %v", names(sections))
		}
	}
}

func TestGetSections_JustPlayedSceneLeavesMoreFromAtOnce(t *testing.T) {
	loadMoreFromConfig(t, 3, []config.Filter{{ID: "smart:recommended", Disabled: true}})
	now := recNow
	svc := recService(&routingStash{recScenes: moreFromJSON()}, &now)
	ctx := context.Background()

	if _, err := svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	svc.markPlayed("3")
	svc.ResetSections()
	sections, err := svc.GetSections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s := sectionByID(sections, "auto:morefrom:p1"); s == nil || fmt.Sprint(s.Ids) != "[2]" {
		t.Fatalf("expected scene 3 gone from More from Alice, got %+v", s)
	}
}

func TestSectionRows_AutoSectionAfterSmartAnchor(t *testing.T) {
	auto := []AutoSection{
		{ID: "studio:s1", Name: "Beta", Ids: []string{"1"}, Count: 1},
		{ID: "auto:morefrom:p1", Name: "More from Alice", Ids: []string{"2"}, Count: 1, After: "recommended"},
		{ID: "auto:morefrom:p2", Name: "More from Cara", Ids: []string{"3"}, Count: 1, After: "recommended"},
		{ID: "auto:morefrom:p3", Name: "More from Dee", Ids: []string{"4"}, Count: 1, After: "nosuch"},
	}
	overrides := []config.Filter{{ID: "smart:recommended"}, {ID: "smart:continue"}}

	rows := sectionRows(nil, smartSections(), auto, overrides)

	got := rowIDs(rows)
	rec := slices.Index(got, "smart:recommended")
	if rec < 0 || got[rec+1] != "auto:morefrom:p1" || got[rec+2] != "auto:morefrom:p2" {
		t.Fatalf("expected the More from rows in order right after Recommended, got %v", got)
	}
	if got[len(got)-2] != "studio:s1" || got[len(got)-1] != "auto:morefrom:p3" {
		t.Fatalf("expected an unanchored auto row last, got %v", got)
	}
}
