package library

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// fam is a family spec switched on with the given threshold and the default
// limit; 0 switches it off.
func fam(minScenes int) familySpec {
	return familySpec{on: minScenes > 0, min: minScenes, limit: config.DefaultFamilyLimit}
}

func TestFamilyFor_OffWithoutRowAndDefaults(t *testing.T) {
	cfg := config.ApplicationConfig{SmartSectionSize: 40}
	if familyFor(cfg, config.FamilyStudio).on {
		t.Fatal("no row: off")
	}
	cfg.Filters = []config.Filter{{ID: config.FamilyStudio}, {ID: config.FamilyMoreFrom, Disabled: true}}
	s := familyFor(cfg, config.FamilyStudio)
	if !s.on || s.min != config.DefaultFamilyMinScenes || s.limit != config.DefaultFamilyLimit || s.size != 40 {
		t.Fatalf("studio defaults = %+v", s)
	}
	if familyFor(cfg, config.FamilyMoreFrom).on {
		t.Fatal("disabled row: off")
	}
	cfg.Filters = []config.Filter{{ID: config.FamilyMoreFrom}}
	if m := familyFor(cfg, config.FamilyMoreFrom); !m.on || m.limit != config.DefaultMoreFromLimit || m.size != 40 {
		t.Fatalf("more-from defaults = %+v", m)
	}
	cfg.Filters = []config.Filter{{ID: config.FamilyMoreFrom, Options: &config.SectionOptions{Limit: intp(3), Size: intp(12)}}}
	if m := familyFor(cfg, config.FamilyMoreFrom); !m.on || m.limit != 3 || m.size != 12 {
		t.Fatalf("more-from options = %+v", m)
	}
}

func TestGroupScenes_LimitPerFamily(t *testing.T) {
	alice := [2]string{"p1", "Alice"}
	scenes := []*groupingScene{
		groupingRow("1", "s1", "Beta", alice),
		groupingRow("2", "s1", "Beta", alice),
		groupingRow("3", "s1", "Beta"),
		groupingRow("4", "s2", "Alpha", alice),
		groupingRow("5", "s2", "Alpha"),
		groupingRow("6", "s3", "Gamma"),
	}
	out := groupScenes(scenes, familySpec{on: true, min: 1, limit: 2}, fam(1))
	if got := fmt.Sprint(autoIDs(out)); got != "[studio:s1 studio:s2 performer:p1]" {
		t.Fatalf("ids = %s", got)
	}
	if out[0].Family != config.FamilyStudio || out[2].Family != config.FamilyPerformer {
		t.Fatalf("families: %+v", out)
	}
}

func TestSectionRows_FamilyRowsListedOffByDefault(t *testing.T) {
	rows := sectionRows(nil, smartSections(), familyDefs(), nil, nil)
	byID := map[string]SectionRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range []string{config.FamilyStudio, config.FamilyPerformer, config.FamilyMoreFrom} {
		r, ok := byID[id]
		if !ok || !r.Family || !r.Disabled || r.Auto || r.Smart {
			t.Fatalf("family %s = %+v ok=%v", id, r, ok)
		}
	}
	ids := rowIDs(rows)
	if slices.Index(ids, config.FamilyMoreFrom) != slices.Index(ids, "smart:recommended")+1 {
		t.Fatalf("more-from family must follow Recommended: %v", ids)
	}
	if ids[len(ids)-2] != config.FamilyStudio || ids[len(ids)-1] != config.FamilyPerformer {
		t.Fatalf("studio and performer families must close the list: %v", ids)
	}
	if byID[config.FamilyStudio].SourceName != "Studio sections" || byID[config.FamilyMoreFrom].Name != "More from your performers" {
		t.Fatalf("family names: %+v %+v", byID[config.FamilyStudio], byID[config.FamilyMoreFrom])
	}
}

func TestSectionRows_ChildrenFollowTheirFamilyRow(t *testing.T) {
	auto := []AutoSection{
		{ID: "studio:s1", Name: "Beta", Family: config.FamilyStudio},
		{ID: "auto:morefrom:p1", Name: "More from Alice", Family: config.FamilyMoreFrom},
		{ID: "studio:s2", Name: "Gamma", Family: config.FamilyStudio},
	}
	overrides := []config.Filter{{ID: config.FamilyStudio}, {ID: config.FamilyMoreFrom}}
	rows := sectionRows(nil, smartSections(), familyDefs(), auto, overrides)
	ids := rowIDs(rows)
	at := func(id string) int { return slices.Index(ids, id) }
	if at("studio:s1") != at(config.FamilyStudio)+1 || at("studio:s2") != at("studio:s1")+1 || at("auto:morefrom:p1") != at(config.FamilyMoreFrom)+1 {
		t.Fatalf("children must sit directly after their family rows: %v", ids)
	}
	for _, r := range rows {
		if r.Auto && (r.Parent != config.FamilyStudio && r.Parent != config.FamilyMoreFrom) {
			t.Fatalf("child without Parent: %+v", r)
		}
	}
	// An overridden child keeps its saved position.
	overrides = []config.Filter{{ID: "studio:s2", Name: "G"}, {ID: config.FamilyStudio}}
	ids = rowIDs(sectionRows(nil, smartSections(), familyDefs(), auto, overrides))
	if at := slices.Index(ids, "studio:s2"); at+1 != slices.Index(ids, config.FamilyStudio) {
		t.Fatalf("overridden child must keep its place before the family row: %v", ids)
	}
}

func TestGetSources_ChildInheritsFamilyHide(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: config.FamilyStudio, HiddenIn: []string{"playa"}}, {ID: "studio:s1", HiddenIn: []string{"deovr"}}})
	svc := NewService(&routingStash{})
	auto := []AutoSection{{ID: "studio:s1", Name: "Beta", Ids: []string{"1"}, Count: 1, Family: config.FamilyStudio}}
	sources, err := svc.getSources(context.Background(), auto)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(sources, func(s sectionSource) bool { return s.row.ID == "studio:s1" })
	if i < 0 || !slices.Equal(sources[i].row.HiddenIn, []string{"deovr", "playa"}) {
		t.Fatalf("child hidden_in = %v (i=%d)", sources, i)
	}
	for _, s := range sources {
		if s.row.Family {
			t.Fatal("family rows are not sources")
		}
	}
}

func TestResolveSections_PerSectionSize(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: "smart:recent", Options: &config.SectionOptions{Size: intp(20)}}})
	stash := &routingStash{}
	svc := NewService(stash)
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q := stash.queryWithSort("created_at"); q == nil || *q.FilterOpts.Per_page != 20 {
		t.Fatalf("Recently added must ask for its own size, got %+v", q)
	}
	if q := stash.queryWithSort("random"); q == nil || *q.FilterOpts.Per_page != 50 {
		t.Fatalf("Random must keep the global size, got %+v", q)
	}
}

func TestSectionRows_OverriddenFamilyStillFollowsDefaultAnchor(t *testing.T) {
	// A migration creates the family row as the only override; Recommended
	// itself has none, so the family keeps following it.
	overrides := []config.Filter{{ID: config.FamilyMoreFrom, Options: &config.SectionOptions{Limit: intp(5)}}}
	ids := rowIDs(sectionRows(nil, smartSections(), familyDefs(), nil, overrides))
	if slices.Index(ids, config.FamilyMoreFrom) != slices.Index(ids, "smart:recommended")+1 {
		t.Fatalf("rows = %v", ids)
	}
	// Once the anchor (and its own anchor) is saved too, the saved order wins.
	overrides = []config.Filter{{ID: config.FamilyMoreFrom}, {ID: "smart:recommended"}, {ID: "smart:continue"}}
	ids = rowIDs(sectionRows(nil, smartSections(), familyDefs(), nil, overrides))
	if slices.Index(ids, config.FamilyMoreFrom)+1 != slices.Index(ids, "smart:recommended") {
		t.Fatalf("rows = %v", ids)
	}
}

func TestSectionRows_MigratedFamilyAloneKeepsDefaultPlacement(t *testing.T) {
	// A migration created the family row; the page was never saved, so no
	// other row is overridden and the family keeps its default place at
	// the end, behind the saved filters, with its children.
	saved := []gql.SavedFilterParts{{Id: "10", Name: "A"}, {Id: "11", Name: "B"}}
	auto := []AutoSection{{ID: "studio:s1", Name: "Beta", Family: config.FamilyStudio}}
	overrides := []config.Filter{{ID: config.FamilyStudio, Options: &config.SectionOptions{MinScenes: intp(20)}}}
	ids := rowIDs(sectionRows(saved, smartSections(), familyDefs(), auto, overrides))
	if got := fmt.Sprint(ids[len(ids)-5:]); got != "[10 11 auto:studio studio:s1 auto:performer]" {
		t.Fatalf("tail = %s", got)
	}
	// Once the page has been saved (any non-family row overridden), the
	// saved order is what counts.
	overrides = append(overrides, config.Filter{ID: "10"})
	ids = rowIDs(sectionRows(saved, smartSections(), familyDefs(), auto, overrides))
	if slices.Index(ids, config.FamilyStudio)+2 != slices.Index(ids, "10") || slices.Index(ids, "studio:s1") != slices.Index(ids, config.FamilyStudio)+1 {
		t.Fatalf("saved order must win: %v", ids)
	}
}
