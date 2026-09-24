package library

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

type groupingScene = gql.FindSceneGroupingsFindScenesFindScenesResultTypeScenesScene
type groupingStudio = gql.FindSceneGroupingsFindScenesFindScenesResultTypeScenesSceneStudio
type groupingPerformer = gql.FindSceneGroupingsFindScenesFindScenesResultTypeScenesScenePerformersPerformer

// groupingRow builds a grouping row: studio "" means no studio, performers are
// {id, name} pairs.
func groupingRow(id, studioID, studioName string, performers ...[2]string) *groupingScene {
	s := &groupingScene{Id: id}
	if studioID != "" {
		s.Studio = &groupingStudio{Id: studioID, Name: studioName}
	}
	for _, p := range performers {
		s.Performers = append(s.Performers, &groupingPerformer{Id: p[0], Name: p[1]})
	}
	return s
}

func autoIDs(sections []AutoSection) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.ID
	}
	return out
}

func loadAutoConfig(t *testing.T, studioMin, performerMin int, filters []config.Filter) {
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
		AutoStudioMin:    studioMin,
		AutoPerformerMin: performerMin,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGroupScenes_ThresholdsAndOrdering(t *testing.T) {
	alice := [2]string{"p1", "Alice"}
	bob := [2]string{"p2", "Bob"}
	cara := [2]string{"p3", "Cara"}
	scenes := []*groupingScene{
		groupingRow("1", "s1", "Beta", alice, bob),
		groupingRow("2", "s2", "Alpha", alice),
		groupingRow("3", "s1", "Beta", alice, cara),
		groupingRow("4", "s2", "Alpha", bob),
		groupingRow("5", "s3", "Gamma"),
		groupingRow("6", "", "", cara, cara), // listed twice: counts once
	}

	got := groupScenes(scenes, 2, 2)

	want := []string{"studio:s2", "studio:s1", "performer:p1", "performer:p2", "performer:p3"}
	if fmt.Sprint(autoIDs(got)) != fmt.Sprint(want) {
		t.Fatalf("ids = %v, want %v", autoIDs(got), want)
	}
	if got[0].Name != "Alpha" || got[0].Count != 2 || fmt.Sprint(got[0].Ids) != "[2 4]" {
		t.Fatalf("studio Alpha wrong: %+v", got[0])
	}
	if got[2].Name != "@ Alice" || got[2].Count != 3 || fmt.Sprint(got[2].Ids) != "[1 2 3]" {
		t.Fatalf("performer Alice wrong: %+v", got[2])
	}
	if got[4].Name != "@ Cara" || fmt.Sprint(got[4].Ids) != "[3 6]" {
		t.Fatalf("performer Cara wrong: %+v", got[4])
	}

	if ids := autoIDs(groupScenes(scenes, 3, 0)); len(ids) != 0 {
		t.Fatalf("no studio reaches 3 scenes and performers are off, got %v", ids)
	}
	if ids := autoIDs(groupScenes(scenes, 0, 3)); fmt.Sprint(ids) != "[performer:p1]" {
		t.Fatalf("only Alice reaches 3 scenes, got %v", ids)
	}
}

func TestGroupScenes_CapsEachKindAtFifty(t *testing.T) {
	var scenes []*groupingScene
	for i := 0; i < 60; i++ {
		// Studio i gets i+1 scenes, so the biggest 50 are studios 10..59.
		for j := 0; j <= i; j++ {
			scenes = append(scenes, groupingRow(fmt.Sprintf("%d-%d", i, j), fmt.Sprintf("s%d", i), fmt.Sprintf("Studio %02d", i), [2]string{fmt.Sprintf("p%d", i), fmt.Sprintf("P%02d", i)}))
		}
	}

	got := groupScenes(scenes, 1, 1)

	if len(got) != 100 {
		t.Fatalf("expected 50 studios and 50 performers, got %d", len(got))
	}
	if got[0].ID != "studio:s59" || got[49].ID != "studio:s10" || got[50].ID != "performer:p59" {
		t.Fatalf("expected the largest first, got %s, %s, %s", got[0].ID, got[49].ID, got[50].ID)
	}
}

func TestSectionRows_AutoRowsAfterEverythingElseAndOverridable(t *testing.T) {
	saved := []gql.SavedFilterParts{{Id: "10", Name: "VR"}}
	auto := []AutoSection{
		{ID: "studio:s1", Name: "Beta", Ids: []string{"1"}, Count: 1},
		{ID: "performer:p1", Name: "@ Alice", Ids: []string{"1"}, Count: 1},
	}
	overrides := []config.Filter{
		{ID: "performer:p1", Name: "Alice", HiddenIn: []string{"playa"}},
		{ID: "performer:gone"}, // dropped below the threshold since saved
	}

	rows := sectionRows(saved, nil, auto, overrides)

	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.ID
	}
	want := []string{"performer:p1", "10", "studio:s1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if r := rows[0]; r.Name != "Alice" || r.SourceName != "@ Alice" || !r.Auto || r.Disabled || !slices.Equal(r.HiddenIn, []string{"playa"}) {
		t.Fatalf("overridden auto row wrong: %+v", r)
	}
	if r := rows[2]; r.Name != "Beta" || !r.Auto || r.Disabled || r.Smart {
		t.Fatalf("default auto row wrong: %+v", r)
	}
	if rows[1].Auto {
		t.Fatal("saved filter must not be marked auto")
	}
}

const twoStudioGroupings = `[
	{"id":"3","studio":{"id":"s1","name":"Beta"},"performers":[{"id":"p1","name":"Alice"}]},
	{"id":"2","studio":{"id":"s2","name":"Alpha"},"performers":[]},
	{"id":"1","studio":{"id":"s1","name":"Beta"},"performers":[{"id":"p1","name":"Alice"}]}
]`

func TestGetSections_AutoStudioSectionUsesGroupingIds(t *testing.T) {
	loadAutoConfig(t, 2, 0, nil)
	stash := &routingStash{groupings: twoStudioGroupings}
	svc := NewService(stash)

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var beta *Section
	for i := range sections {
		if sections[i].ID == "studio:s1" {
			beta = &sections[i]
		}
	}
	if beta == nil {
		t.Fatalf("expected a studio section for Beta, got %v", names(sections))
	}
	if beta.Name != "Beta" || fmt.Sprint(beta.Ids) != "[3 1]" {
		t.Fatalf("Beta section wrong: %+v", *beta)
	}
	if last := sections[len(sections)-1]; last.ID != "studio:s1" {
		t.Fatalf("auto sections come last, got %s", last.ID)
	}
	// Only the three default smart sections query scene ids.
	if n := stash.sceneIdQueries(); n != 3 {
		t.Fatalf("expected 3 scene-id queries (smart only), got %d", n)
	}
	if n := stash.groupingQueries(); n != 1 {
		t.Fatalf("expected one grouping query, got %d", n)
	}

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.SectionRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := stash.groupingQueries(); n != 1 {
		t.Fatalf("grouping must be cached with the index, got %d queries", n)
	}
	last := rows[len(rows)-1]
	if last.ID != "studio:s1" || !last.Auto {
		t.Fatalf("Sections page should list the auto row last, got %+v", last)
	}
}

func TestGetSections_NoGroupingQueryWhenOff(t *testing.T) {
	loadAutoConfig(t, 0, 0, nil)
	stash := &routingStash{groupings: twoStudioGroupings}
	svc := NewService(stash)

	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SectionRows(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := stash.groupingQueries(); n != 0 {
		t.Fatalf("expected no grouping query with both thresholds at 0, got %d", n)
	}
}

func TestGetSavedFilterSceneSetsFor_AutoSectionsAreCategoriesUnlessHidden(t *testing.T) {
	loadAutoConfig(t, 1, 2, []config.Filter{{ID: "studio:s2", HiddenIn: []string{"playa"}}})
	svc := NewService(&routingStash{groupings: twoStudioGroupings})

	sets, err := svc.GetSavedFilterSceneSetsFor(context.Background(), "playa")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, len(sets))
	for i, s := range sets {
		ids[i] = s.ID
	}
	if !slices.Contains(ids, "studio:s1") || !slices.Contains(ids, "performer:p1") {
		t.Fatalf("expected auto sections as Playa categories, got %v", ids)
	}
	if slices.Contains(ids, "studio:s2") {
		t.Fatalf("studio:s2 is hidden in Playa, got %v", ids)
	}
	for _, s := range sets {
		if s.ID == "performer:p1" && s.Name != "@ Alice" {
			t.Fatalf("performer set named %q", s.Name)
		}
	}
}

func TestAutoSectionCount_ReadsTheCachedIndex(t *testing.T) {
	loadAutoConfig(t, 2, 0, nil)
	stash := &routingStash{groupings: twoStudioGroupings}
	svc := NewService(stash)
	if n := svc.AutoSectionCount(); n != 0 {
		t.Fatalf("no index yet must count 0, got %d", n)
	}
	if stash.groupingQueries() != 0 {
		t.Fatal("counting must not build the index")
	}
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	auto, err := svc.autoSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := svc.AutoSectionCount(); n != len(auto) || n == 0 {
		t.Fatalf("count %d want %d", n, len(auto))
	}
	svc.ResetSections()
	if n := svc.AutoSectionCount(); n != 0 {
		t.Fatalf("a reset index counts 0, got %d", n)
	}
}
