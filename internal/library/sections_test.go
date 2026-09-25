package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// sceneIdQuery mirrors genqlient's (unexported) variables struct for
// FindSceneIdsByFilter, decoded from the request's JSON form.
type sceneIdQuery struct {
	Scene_filter *gql.SceneFilterType `json:"scene_filter"`
	FilterOpts   *gql.FindFilterType  `json:"filterOpts"`
}

// routingStash has no saved filters and records every scene-id query.
type routingStash struct {
	mu       sync.Mutex
	queries  []*sceneIdQuery
	allIds   int
	tagLoads int

	// groupings is the FindSceneGroupings answer (scenes array JSON);
	// groupingLoads counts how often it was asked.
	groupings     string
	groupingLoads int

	// gate and started let a test pause a build in flight: when gate is
	// non-nil, a FindSceneIdsByFilter query closes started (once, on the
	// first such query) and then blocks until gate is closed. Both are nil
	// in every test but the one that uses them, so they must never be read
	// unguarded or they would block the other tests.
	gate        chan struct{}
	started     chan struct{}
	startedOnce sync.Once

	// recScenes is the FindRecommendationScenes answer (scenes array JSON);
	// recLoads counts how often it was asked, recErr fails it.
	recScenes string
	recLoads  int
	recErr    error
}

func (r *routingStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		r.mu.Lock()
		r.tagLoads++
		r.mu.Unlock()
		payload = `{"findTags":{"tags":[]}}`
	case "FindSceneGroupings":
		r.mu.Lock()
		r.groupingLoads++
		scenes := r.groupings
		r.mu.Unlock()
		if scenes == "" {
			scenes = "[]"
		}
		payload = `{"findScenes":{"scenes":` + scenes + `}}`
	case "FindRecommendationScenes":
		r.mu.Lock()
		r.recLoads++
		scenes, err := r.recScenes, r.recErr
		r.mu.Unlock()
		if err != nil {
			return err
		}
		if scenes == "" {
			scenes = "[]"
		}
		payload = `{"findScenes":{"scenes":` + scenes + `}}`
	case "FindAllSceneIds":
		r.mu.Lock()
		r.allIds++
		r.mu.Unlock()
		payload = `{"findScenes":{"scenes":[{"id":"1"},{"id":"2"}]}}`
	case "FindSceneIdsByFilter":
		if r.gate != nil {
			r.startedOnce.Do(func() { close(r.started) })
			<-r.gate
		}
		raw, err := json.Marshal(req.Variables)
		if err != nil {
			return err
		}
		in := &sceneIdQuery{}
		if err := json.Unmarshal(raw, in); err != nil {
			return err
		}
		r.mu.Lock()
		r.queries = append(r.queries, in)
		n := len(r.queries)
		r.mu.Unlock()
		payload = fmt.Sprintf(`{"findScenes":{"scenes":[{"id":"%d"}]}}`, n)
	case "FindScenes":
		raw, _ := json.Marshal(req.Variables)
		var in struct {
			SceneIDs []int `json:"scene_ids"`
		}
		_ = json.Unmarshal(raw, &in)
		scenes := make([]string, 0, len(in.SceneIDs))
		for _, id := range in.SceneIDs {
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"S%d","created_at":"2024-01-01T00:00:00Z","files":[],"tags":[]}`, id, id))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func (r *routingStash) sceneIdQueries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

func (r *routingStash) recQueries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recLoads
}

func (r *routingStash) groupingQueries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.groupingLoads
}

func (r *routingStash) allIdQueries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.allIds
}

func (r *routingStash) tagQueries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tagLoads
}

func loadConfig(t *testing.T, filters []config.Filter) {
	t.Helper()
	err := config.Load(config.ApplicationConfig{
		ListenAddress:    ":9666",
		StashGraphQLUrl:  "http://stash:9999/graphql",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		ConfigPath:       t.TempDir(),
		Filters:          filters,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func names(sections []Section) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.Name
	}
	return out
}

func (r *routingStash) queryWithSort(sort string) *sceneIdQuery {
	for _, q := range r.queries {
		if q.FilterOpts != nil && q.FilterOpts.Sort != nil && *q.FilterOpts.Sort == sort {
			return q
		}
	}
	return nil
}

func TestGetSections_DefaultSmartSectionsWithoutSavedFilters(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{}
	svc := NewService(stash)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Continue watching", "Recently added", "Random"}
	if got := names(sections); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	cont := stash.queryWithSort("last_played_at")
	if cont == nil || cont.Scene_filter.Resume_time == nil || cont.Scene_filter.Resume_time.Modifier != gql.CriterionModifierGreaterThan || cont.Scene_filter.Resume_time.Value != 0 {
		t.Fatalf("continue watching query missing resume_time > 0: %+v", cont)
	}
	if *cont.FilterOpts.Per_page != 50 {
		t.Fatalf("expected per_page 50, got %d", *cont.FilterOpts.Per_page)
	}
	if stash.queryWithSort("random") == nil || stash.queryWithSort("created_at") == nil {
		t.Fatal("expected random and created_at queries")
	}
}

func TestGetSections_OverridesOrderAndHideSmartSections(t *testing.T) {
	loadConfig(t, []config.Filter{
		{ID: "smart:random"},
		{ID: "smart:recent", Disabled: true},
		{ID: "smart:unwatched", Name: "Not yet seen"},
	})
	svc := NewService(&routingStash{})

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Continue watching", "Random", "Not yet seen"}
	if got := names(sections); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
}

func TestSectionRows_MixesSavedFiltersAndSmartSections(t *testing.T) {
	saved := []gql.SavedFilterParts{{Id: "10", Name: "VR Blowjob"}, {Id: "11", Name: "2D Anal"}}
	overrides := []config.Filter{
		{ID: "11", Name: "Anal (2D)"},
		{ID: "smart:random"},
		{ID: "99"}, // deleted in Stash since the override was saved
		{ID: "smart:recent", Disabled: true},
	}

	rows := sectionRows(saved, smartSections(), nil, overrides)

	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.ID
	}
	// Un-overridden smart sections first (continue, recommended after it,
	// unwatched, toprated, withscript, noscript), then overrides in order
	// (99 dropped), then the remaining saved filter.
	want := []string{"smart:continue", "smart:recommended", "smart:unwatched", "smart:toprated", "smart:withscript", "smart:noscript", "11", "smart:random", "smart:recent", "10"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if rows[6].Name != "Anal (2D)" || rows[6].SourceName != "2D Anal" || rows[6].Smart {
		t.Fatalf("renamed saved filter row wrong: %+v", rows[6])
	}
	if !rows[2].Disabled {
		t.Fatal("unwatched should be off by default")
	}
	if !rows[8].Disabled {
		t.Fatal("recent was disabled by its override")
	}
}

func TestGetSavedFilterSceneSets_OmitsSmartRandomForPlaya(t *testing.T) {
	loadConfig(t, nil)
	svc := NewService(&routingStash{})

	sets, err := svc.GetSavedFilterSceneSets(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range sets {
		if s.ID == "smart:random" {
			t.Fatal("smart:random must not be exposed to Playa")
		}
	}
	if len(sets) != 2 {
		t.Fatalf("expected continue and recent for Playa, got %+v", sets)
	}
}

func TestSectionRows_ListsEverySmartSectionWithDefaults(t *testing.T) {
	loadConfig(t, nil)
	svc := NewService(&routingStash{})

	rows, err := svc.SectionRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := "[smart:continue smart:recommended smart:recent smart:unwatched smart:toprated smart:random smart:withscript smart:noscript]"
	if got := rowIDs(rows); fmt.Sprint(got) != want {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if rows[0].Disabled || !rows[0].Smart {
		t.Fatalf("first row should be enabled smart continue, got %+v", rows[0])
	}
	if r := rows[1]; r.Name != "Recommended for you" || r.Disabled || !r.Smart {
		t.Fatalf("recommended should be on by default, got %+v", r)
	}
	if !rows[3].Disabled {
		t.Fatalf("unwatched should be off by default, got %+v", rows[3])
	}
}

func TestGetSections_ServesCachedSetsWithinTTL(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{}
	svc := NewService(stash)

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := stash.sceneIdQueries()
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stash.sceneIdQueries(); got != first {
		t.Fatalf("second call within the TTL must not requery, got %d queries after %d", got, first)
	}

	svc.ResetSections()
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stash.sceneIdQueries(); got != 2*first {
		t.Fatalf("after ResetSections the sets must be rebuilt, got %d queries", got)
	}
}

func TestGetSections_ConcurrentCallersShareOneBuild(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{}
	svc := NewService(stash)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.GetSections(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if got := stash.sceneIdQueries(); got != 3 {
		t.Fatalf("expected one build (3 smart queries), got %d", got)
	}
	if got := stash.tagQueries(); got != 1 {
		t.Fatalf("expected the tag reload to run once for the shared build, got %d", got)
	}
}

func TestGetSections_AllFallbackIsCachedWithinTTL(t *testing.T) {
	loadConfig(t, []config.Filter{
		{ID: "smart:continue", Disabled: true},
		{ID: "smart:recent", Disabled: true},
		{ID: "smart:random", Disabled: true},
	})
	stash := &routingStash{}
	svc := NewService(stash)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := names(sections); fmt.Sprint(got) != fmt.Sprint([]string{"All"}) {
		t.Fatalf("sections = %v, want [All]", got)
	}

	sections, err = svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := names(sections); fmt.Sprint(got) != fmt.Sprint([]string{"All"}) {
		t.Fatalf("sections = %v, want [All]", got)
	}

	if got := stash.allIdQueries(); got != 1 {
		t.Fatalf("expected the All fallback to be cached within the TTL, got %d FindAllSceneIds queries", got)
	}
}

func TestGetScenes_SeedsIndexWhenCacheIsEmpty(t *testing.T) {
	loadConfig(t, nil)
	svc := NewService(&routingStash{})

	scenes, err := svc.GetScenes(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(scenes) == 0 {
		t.Fatal("expected GetScenes to build the index and return scenes without a prior GetSections call")
	}
}

func TestResetSectionsDuringBuildForcesRebuild(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{gate: make(chan struct{}), started: make(chan struct{})}
	svc := NewService(stash)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := svc.GetSections(context.Background()); err != nil {
			t.Error(err)
		}
	}()

	// Wait until the build has reached Stash, then reset while it is still
	// in flight, then let it finish.
	<-stash.started
	svc.ResetSections()
	close(stash.gate)
	<-done

	first := stash.sceneIdQueries()
	if first == 0 {
		t.Fatal("expected the first build to have queried Stash")
	}

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stash.sceneIdQueries(); got != 2*first {
		t.Fatalf("expected a reset mid-build to force a second build, got %d scene-id queries (was %d)", got, first)
	}
}

func TestGetSavedFilterSceneSets_SharesIndexWithGetSections(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{}
	svc := NewService(stash)

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := stash.sceneIdQueries()

	if _, err := svc.GetSavedFilterSceneSets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stash.sceneIdQueries(); got != before {
		t.Fatalf("expected GetSavedFilterSceneSets to reuse the index GetSections already built, got %d queries after %d", got, before)
	}
}

func TestGetSectionsFor_DropsHiddenPlayers(t *testing.T) {
	// The fake has no saved filters, so two smart sections stand in for
	// "A" and "B": Recently added is hidden in Playa only.
	loadConfig(t, []config.Filter{
		{ID: "smart:continue"},
		{ID: "smart:recent", HiddenIn: []string{"playa"}},
		{ID: "smart:random", Disabled: true},
	})
	svc := NewService(&routingStash{})

	all, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hs, err := svc.GetSectionsFor(context.Background(), "heresphere")
	if err != nil {
		t.Fatal(err)
	}
	playa, err := svc.GetSavedFilterSceneSetsFor(context.Background(), "playa")
	if err != nil {
		t.Fatal(err)
	}
	playaSections, err := svc.GetSectionsFor(context.Background(), "playa")
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 2 || len(hs) != 2 {
		t.Fatalf("all=%d heresphere=%d, want 2 and 2", len(all), len(hs))
	}
	if all[1].ID != "smart:recent" || fmt.Sprint(all[1].HiddenIn) != fmt.Sprint([]string{"playa"}) {
		t.Fatalf("GetSections should carry the id and hidden_in, got %+v", all[1])
	}
	if len(playa) != 1 || playa[0].ID != "smart:continue" {
		t.Fatalf("playa should only see Continue watching, got %+v", playa)
	}
	if len(playaSections) != 1 || playaSections[0].ID != "smart:continue" {
		t.Fatalf("playa sections should only hold Continue watching, got %+v", playaSections)
	}
}

func TestHiddenFor(t *testing.T) {
	if HiddenFor(nil, "playa") || HiddenFor([]string{"deovr"}, "playa") {
		t.Fatal("a player not listed must not be hidden")
	}
	if !HiddenFor([]string{"deovr", "playa"}, "playa") {
		t.Fatal("a listed player must be hidden")
	}
}

func TestGetSectionsFor_FallsBackWhenEverythingIsHidden(t *testing.T) {
	loadConfig(t, []config.Filter{
		{ID: "smart:continue", HiddenIn: []string{"deovr"}},
		{ID: "smart:recent", HiddenIn: []string{"deovr"}},
		{ID: "smart:random", Disabled: true},
	})
	svc := NewService(&routingStash{})

	deovr, err := svc.GetSectionsFor(context.Background(), "deovr")
	if err != nil {
		t.Fatal(err)
	}
	if len(deovr) != 2 {
		t.Fatalf("expected the full index when every section is hidden for a player, got %d", len(deovr))
	}
	sets, err := svc.GetSavedFilterSceneSetsFor(context.Background(), "deovr")
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 {
		t.Fatalf("expected all sets for the same reason, got %d", len(sets))
	}
}

func rowIDs(rows []SectionRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestSectionRows_SmartSectionWithAfterFollowsItsAnchor(t *testing.T) {
	smart := []SmartSection{
		{Key: "a", Name: "A", Default: true},
		{Key: "b", Name: "B", Default: true},
		{Key: "new", Name: "New", Default: true, After: "a"},
		{Key: "c", Name: "C"},
	}
	saved := []gql.SavedFilterParts{{Id: "10", Name: "Landing"}}

	// No overrides: the anchor is a default row, the new one follows it.
	if got := rowIDs(sectionRows(saved, smart, nil, nil)); fmt.Sprint(got) != "[smart:a smart:new smart:b smart:c 10]" {
		t.Fatalf("default rows = %v", got)
	}

	// Existing overrides: the landing row stays first, the new section
	// follows its overridden anchor instead of jumping to the top.
	overrides := []config.Filter{{ID: "10"}, {ID: "smart:b"}, {ID: "smart:a", Name: "Renamed"}, {ID: "smart:c", Disabled: true}}
	rows := sectionRows(saved, smart, nil, overrides)
	if got := rowIDs(rows); fmt.Sprint(got) != "[10 smart:b smart:a smart:new smart:c]" {
		t.Fatalf("overridden rows = %v", got)
	}
	if r := rows[3]; r.Name != "New" || r.Disabled || !r.Smart {
		t.Fatalf("placed row wrong: %+v", r)
	}

	// An override for the new section itself wins over After.
	overrides = append([]config.Filter{{ID: "smart:new", Disabled: true}}, overrides...)
	if got := rowIDs(sectionRows(saved, smart, nil, overrides)); fmt.Sprint(got) != "[smart:new 10 smart:b smart:a smart:c]" {
		t.Fatalf("rows with override for new = %v", got)
	}

	// An anchor that does not exist falls back to the default placement.
	smart[2].After = "missing"
	if got := rowIDs(sectionRows(saved, smart, nil, overrides[1:])); fmt.Sprint(got) != "[smart:new 10 smart:b smart:a smart:c]" {
		t.Fatalf("rows with missing anchor = %v", got)
	}
}

func TestSectionRows_SeveralSectionsAfterOneAnchorKeepTheirOrder(t *testing.T) {
	smart := []SmartSection{
		{Key: "a", Name: "A", Default: true},
		{Key: "x", Name: "X", Default: true, After: "a"},
		{Key: "y", Name: "Y", Default: true, After: "a"},
	}
	overrides := []config.Filter{{ID: "smart:a"}}
	if got := rowIDs(sectionRows(nil, smart, nil, overrides)); fmt.Sprint(got) != "[smart:a smart:x smart:y]" {
		t.Fatalf("rows = %v", got)
	}
}

func TestSectionRows_ChainedAfterFollowsPlacedAnchor(t *testing.T) {
	smart := []SmartSection{
		{Key: "a", Name: "A", Default: true},
		{Key: "x", Name: "X", Default: true, After: "a"},
		{Key: "y", Name: "Y", Default: true, After: "x"},
		{Key: "z", Name: "Z", Default: true, After: "w"},
		{Key: "w", Name: "W", Default: true, After: "a"},
	}
	overrides := []config.Filter{{ID: "10"}, {ID: "smart:a"}}
	saved := []gql.SavedFilterParts{{Id: "10", Name: "Landing"}}
	// z's anchor w is placed only after z, so z falls back to the top.
	if got := rowIDs(sectionRows(saved, smart, nil, overrides)); fmt.Sprint(got) != "[smart:z 10 smart:a smart:x smart:y smart:w]" {
		t.Fatalf("rows = %v", got)
	}
}
