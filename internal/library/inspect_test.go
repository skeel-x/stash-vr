package library

import (
	"context"
	"testing"

	"stash-vr/internal/config"
)

func TestMatchingRules_ReportsEveryMatchingRuleInOrder(t *testing.T) {
	rules := []config.VideoRule{{Tag: "FISHEYE"}, {Tag: "DOME"}, {Tag: "passthrough"}, {Tag: "FISHEYE", Fov: 190}}
	got := MatchingRules(rules, tags("Passthrough", "FISHEYE", "Blonde"))
	if len(got) != 3 || got[0] != 0 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("got %v", got)
	}
	if got := MatchingRules(rules, nil); len(got) != 0 {
		t.Fatalf("no tags must match nothing, got %v", got)
	}
}

func TestProfileSourceFor_Precedence(t *testing.T) {
	has := func(ids ...string) func(string) bool {
		return func(id string) bool {
			for _, x := range ids {
				if x == id {
					return true
				}
			}
			return false
		}
	}
	studio := func(id string) func() string { return func() string { return id } }
	cases := []struct {
		name    string
		f       Format
		has     func(string) bool
		learned func() string
		source  string
		scene   string
	}{
		{"own", Format{ProfileScene: "42", Generated: true}, has("9", "42", "7"), studio("7"), ProfileOwn, "9"},
		{"studio beats rule", Format{ProfileScene: "42", Generated: true}, has("42", "7"), studio("7"), ProfileStudio, "7"},
		{"studio beats generated", Format{Generated: true}, has("7"), studio("7"), ProfileStudio, "7"},
		{"no studio profile", Format{ProfileScene: "42", Generated: true}, has("42"), studio(""), ProfileRule, "42"},
		{"rule", Format{ProfileScene: "42", Generated: true}, has("42"), nil, ProfileRule, "42"},
		{"rule profile missing falls to generated", Format{ProfileScene: "42", Generated: true}, has(), nil, ProfileGenerated, "9"},
		{"generated", Format{Generated: true}, has(), nil, ProfileGenerated, "9"},
		{"none", Format{ProfileScene: "42"}, has(), nil, ProfileNone, ""},
		{"nil lookup", Format{Generated: true}, nil, nil, ProfileGenerated, "9"},
	}
	for _, c := range cases {
		source, scene := ProfileSourceFor("9", c.f, c.has, c.learned)
		if source != c.source || scene != c.scene {
			t.Errorf("%s: got %s/%s want %s/%s", c.name, source, scene, c.source, c.scene)
		}
	}
}

func TestProfileSourceFor_LooksUpTheStudioProfileOnlyWithoutAnOwn(t *testing.T) {
	called := false
	learned := func() string { called = true; return "7" }

	ProfileSourceFor("9", Format{}, func(id string) bool { return id == "9" }, learned)

	if called {
		t.Fatal("a scene with its own profile must not look up a studio profile")
	}
}

func TestSearchCachedScenes_MatchesTitlesWithoutQuerying(t *testing.T) {
	loadConfig(t, nil)
	svc := NewService(&indexStash{ids: []string{"1", "2", "10", "11", "12", "13", "14", "3"}})
	if got := svc.SearchCachedScenes("scene", 5); len(got) != 0 {
		t.Fatalf("an empty cache finds nothing, got %d", len(got))
	}
	if _, err := svc.GetScenes(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := svc.SearchCachedScenes("  SCENE 1 ", 5)
	ids := make([]string, len(got))
	for i, vd := range got {
		ids[i] = vd.Id()
	}
	want := []string{"1", "10", "11", "12", "13"}
	if len(ids) != len(want) {
		t.Fatalf("got %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v want %v", ids, want)
		}
	}
	if got := svc.SearchCachedScenes("scene 3", 5); len(got) != 1 || got[0].Id() != "3" {
		t.Fatalf("scene 3: %v", got)
	}
	if got := svc.SearchCachedScenes("", 5); len(got) != 0 {
		t.Fatal("an empty query matches nothing")
	}
}
