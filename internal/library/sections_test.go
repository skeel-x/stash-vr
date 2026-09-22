package library

import (
	"context"
	"encoding/json"
	"fmt"
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
	mu      sync.Mutex
	queries []*sceneIdQuery
}

func (r *routingStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindSceneIdsByFilter":
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
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
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

	rows := sectionRows(saved, smartSections(), overrides)

	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.ID
	}
	// Un-overridden smart sections first (continue, unwatched, toprated, withscript, noscript),
	// then overrides in order (99 dropped), then the remaining saved filter.
	want := []string{"smart:continue", "smart:unwatched", "smart:toprated", "smart:withscript", "smart:noscript", "11", "smart:random", "smart:recent", "10"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if rows[5].Name != "Anal (2D)" || rows[5].SourceName != "2D Anal" || rows[5].Smart {
		t.Fatalf("renamed saved filter row wrong: %+v", rows[5])
	}
	if !rows[7].Disabled || rows[1].Disabled == false && rows[1].ID != "smart:unwatched" {
		t.Fatalf("disabled flags wrong: %+v", rows)
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

	if len(rows) != 7 {
		t.Fatalf("expected 7 smart rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].ID != "smart:continue" || rows[0].Disabled || !rows[0].Smart {
		t.Fatalf("first row should be enabled smart continue, got %+v", rows[0])
	}
	if rows[2].ID != "smart:unwatched" || !rows[2].Disabled {
		t.Fatalf("unwatched should be off by default, got %+v", rows[2])
	}
}
