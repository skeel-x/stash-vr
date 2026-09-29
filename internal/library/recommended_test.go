package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// recJSON is a library where scene 1 was watched yesterday with performer
// p1; 2 and 3 share that performer and have not been played, 3 is newer.
func recJSON() string {
	yesterday := recNow.Add(-24 * time.Hour).Format(time.RFC3339)
	file := `"files":[{"width":1920,"height":1080}]`
	return `[
		{"id":"3","tags":[],"performers":[{"id":"p1"}],` + file + `},
		{"id":"2","tags":[],"performers":[{"id":"p1"}],` + file + `},
		{"id":"1","tags":[],"performers":[{"id":"p1"}],"play_count":1,"last_played_at":"` + yesterday + `",` + file + `},
		{"id":"4","tags":[{"id":"t","name":"t"}],"performers":[],` + file + `},
		{"id":"5","tags":[{"id":"u","name":"u"}],"performers":[],` + file + `},
		{"id":"6","tags":[{"id":"v","name":"v"}],"performers":[],` + file + `},
		{"id":"7","tags":[{"id":"w","name":"w"}],"performers":[],` + file + `}
	]`
}

// recService returns a service whose clock reads *now.
func recService(stash *routingStash, now *time.Time) *Service {
	svc := NewService(stash)
	svc.now = func() time.Time { return *now }
	return svc
}

func sectionByID(sections []Section, id string) *Section {
	for i := range sections {
		if sections[i].ID == id {
			return &sections[i]
		}
	}
	return nil
}

func TestGetSections_RecommendedFollowsContinueWatching(t *testing.T) {
	loadConfig(t, nil)
	now := recNow
	svc := recService(&routingStash{recScenes: recJSON()}, &now)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Continue watching", "Recommended for you", "Recently added", "Random"}
	if got := names(sections); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	if rec := sections[1]; rec.ID != "smart:recommended" || fmt.Sprint(rec.Ids) != "[3 2]" {
		t.Fatalf("recommended section wrong: %+v", rec)
	}

	sets, err := svc.GetSavedFilterSceneSets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sets {
		found = found || s.ID == "smart:recommended"
	}
	if !found {
		t.Fatalf("recommended should be listed for Playa, got %+v", sets)
	}
}

func TestGetSections_RecommendedUsesSmartSectionSize(t *testing.T) {
	loadConfig(t, nil)
	cfg := config.Application()
	cfg.SmartSectionSize = 10
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	var scenes []string
	file := `"files":[{"width":1920,"height":1080}]`
	for i := 0; i < 15; i++ {
		scenes = append(scenes, fmt.Sprintf(`{"id":"%d","tags":[],"performers":[{"id":"p1"}],%s}`, i+10, file))
	}
	for i := 0; i < 30; i++ {
		scenes = append(scenes, fmt.Sprintf(`{"id":"%d","tags":[{"id":"f%d","name":"f%d"}],"performers":[],%s}`, i+100, i, i, file))
	}
	scenes = append(scenes, `{"id":"1","tags":[],"performers":[{"id":"p1"}],"play_count":1,"last_played_at":"`+recNow.Format(time.RFC3339)+`",`+file+`}`)
	now := recNow
	svc := recService(&routingStash{recScenes: "[" + strings.Join(scenes, ",") + "]"}, &now)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec := sectionByID(sections, "smart:recommended")
	if rec == nil || len(rec.Ids) != 10 || rec.Ids[0] != "10" {
		t.Fatalf("expected the 10 newest matches, got %+v", rec)
	}
}

func TestGetSections_RecommendedLeftOutWithoutHistory(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{recScenes: `[{"id":"1","tags":[],"performers":[],"files":[{"width":1,"height":1}]}]`}
	now := recNow
	svc := recService(stash, &now)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if sectionByID(sections, "smart:recommended") != nil {
		t.Fatalf("an empty recommendation must be left out, got %v", names(sections))
	}
	if stash.recQueries() != 1 {
		t.Fatalf("expected one recommendation query, got %d", stash.recQueries())
	}
}

func TestGetSections_RecommendedCachedForHalfAnHour(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{recScenes: recJSON()}
	now := recNow
	svc := recService(stash, &now)
	ctx := context.Background()

	if _, err := svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	// The index is rebuilt every 60 s; the recommendation is not.
	now = now.Add(29 * time.Minute)
	svc.ResetSections()
	sections, err := svc.GetSections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := stash.recQueries(); got != 1 {
		t.Fatalf("expected the recommendation cached across index rebuilds, got %d queries", got)
	}
	if rec := sectionByID(sections, "smart:recommended"); rec == nil || fmt.Sprint(rec.Ids) != "[3 2]" {
		t.Fatalf("cached recommendation wrong: %+v", rec)
	}

	now = now.Add(2 * time.Minute)
	svc.ResetSections()
	if _, err := svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stash.recQueries(); got != 2 {
		t.Fatalf("expected a new query after 30 minutes, got %d", got)
	}

	svc.ResetCaches()
	if _, err := svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stash.recQueries(); got != 3 {
		t.Fatalf("expected ResetCaches to drop the recommendation, got %d queries", got)
	}
}

func TestGetSections_RecommendedRecomputedWhenSizeChanges(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{recScenes: recJSON()}
	now := recNow
	svc := recService(stash, &now)

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Application()
	cfg.SmartSectionSize = 20
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	svc.ResetSections()
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stash.recQueries(); got != 2 {
		t.Fatalf("expected a new query for a new section size, got %d", got)
	}
}

func TestGetSections_RecommendedQueryErrorSkipsSectionAndRetries(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{recErr: errors.New("boom")}
	now := recNow
	svc := recService(stash, &now)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sectionByID(sections, "smart:recommended") != nil || len(sections) != 3 {
		t.Fatalf("a failed recommendation must only cost its own section, got %v", names(sections))
	}

	svc.ResetSections()
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stash.recQueries(); got != 2 {
		t.Fatalf("a failure must not be cached, got %d queries", got)
	}
}

func TestGetSections_EmptyRecommendationKeepsAllFallback(t *testing.T) {
	loadConfig(t, []config.Filter{
		{ID: "smart:continue", Disabled: true},
		{ID: "smart:recent", Disabled: true},
		{ID: "smart:random", Disabled: true},
	})
	now := recNow
	svc := recService(&routingStash{}, &now)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := names(sections); fmt.Sprint(got) != "[All]" {
		t.Fatalf("with only an empty recommendation enabled, expected [All], got %v", got)
	}

	svc = recService(&routingStash{recScenes: recJSON()}, &now)
	sections, err = svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := names(sections); fmt.Sprint(got) != "[Recommended for you]" {
		t.Fatalf("expected only the recommendation, got %v", got)
	}
}

func TestSectionRows_RecommendedJoinsExistingOrderAfterContinue(t *testing.T) {
	saved := []gql.SavedFilterParts{{Id: "10", Name: "Landing"}, {Id: "11", Name: "Other"}}
	// An installation that saved its order before the section existed.
	overrides := []config.Filter{
		{ID: "10"},
		{ID: "smart:random"},
		{ID: "smart:continue"},
		{ID: "11"},
		{ID: "smart:recent"},
		{ID: "smart:unwatched", Disabled: true},
		{ID: "smart:toprated", Disabled: true},
		{ID: "smart:withscript", Disabled: true},
		{ID: "smart:noscript", Disabled: true},
	}

	rows := sectionRows(saved, smartSections(), nil, overrides)

	want := "[10 smart:random smart:continue smart:recommended 11 smart:recent smart:unwatched smart:toprated smart:withscript smart:noscript]"
	if got := rowIDs(rows); fmt.Sprint(got) != want {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if rows[3].Disabled {
		t.Fatal("recommended is on by default")
	}
}

func TestGetSections_JustPlayedSceneLeavesRecommendedAtOnce(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{recScenes: recJSON()}
	now := recNow
	svc := recService(stash, &now)
	ctx := context.Background()

	sections, err := svc.GetSections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rec := sectionByID(sections, "smart:recommended"); rec == nil || fmt.Sprint(rec.Ids) != "[3 2]" {
		t.Fatalf("recommended section wrong: %+v", rec)
	}

	// A stored resume position puts 3 in Continue watching: it must not sit
	// in Recommended as well until the cache expires.
	resume := 120.0
	if err := svc.SaveActivity(ctx, "3", nil, &resume); err != nil {
		t.Fatal(err)
	}
	svc.ResetSections()
	sections, err = svc.GetSections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rec := sectionByID(sections, "smart:recommended"); rec == nil || fmt.Sprint(rec.Ids) != "[2]" {
		t.Fatalf("expected 3 gone from the cached recommendation, got %+v", rec)
	}
	if got := stash.recQueries(); got != 1 {
		t.Fatalf("the cache must be filtered, not recomputed, got %d queries", got)
	}

	// A cleared position (finished or barely started) is not a play, and a
	// write Stash refused leaves the scene in the list.
	zero := 0.0
	if err := svc.SaveActivity(ctx, "2", nil, &zero); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveActivity(ctx, "2", &resume, nil); err != nil {
		t.Fatal(err)
	}
	stash.mu.Lock()
	stash.writeErr = errors.New("stash down")
	stash.mu.Unlock()
	if err := svc.IncrementPlayCount(ctx, "2"); err == nil {
		t.Fatal("expected the failed write reported")
	}
	svc.ResetSections()
	if sections, err = svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if rec := sectionByID(sections, "smart:recommended"); rec == nil || fmt.Sprint(rec.Ids) != "[2]" {
		t.Fatalf("expected 2 still recommended, got %+v", rec)
	}

	// A counted play removes it too; with nothing left the section goes.
	stash.mu.Lock()
	stash.writeErr = nil
	stash.mu.Unlock()
	if err := svc.IncrementPlayCount(ctx, "2"); err != nil {
		t.Fatal(err)
	}
	svc.ResetSections()
	if sections, err = svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if sectionByID(sections, "smart:recommended") != nil {
		t.Fatalf("expected the emptied recommendation left out, got %v", names(sections))
	}

	// Once the cache expires the fresh query decides, and the old marks
	// are forgotten (the fake still lists both as unplayed).
	now = now.Add(recCacheTTL + time.Minute)
	svc.ResetSections()
	if sections, err = svc.GetSections(ctx); err != nil {
		t.Fatal(err)
	}
	if rec := sectionByID(sections, "smart:recommended"); rec == nil || fmt.Sprint(rec.Ids) != "[3 2]" {
		t.Fatalf("expected the fresh recommendation, got %+v", rec)
	}
	if got := stash.recQueries(); got != 2 {
		t.Fatalf("expected one recomputation, got %d", got)
	}
	svc.muRec.Lock()
	marks := len(svc.recPlayed)
	svc.muRec.Unlock()
	if marks != 0 {
		t.Fatalf("expected the expired marks pruned, %d left", marks)
	}
}
