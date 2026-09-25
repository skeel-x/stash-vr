package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
)

// studioScene is a scene studioStash knows: its studio id ("" for none)
// and tag names.
type studioScene struct {
	studio string
	tags   []string
}

// studioStash answers FindScenes with the known scenes among the ids
// asked for and counts the queries.
type studioStash struct {
	mu     sync.Mutex
	scenes map[int]studioScene
	calls  int
}

func (s *studioStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	if req.OpName != "FindScenes" {
		return json.Unmarshal([]byte(`{}`), resp.Data)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	raw, _ := json.Marshal(req.Variables)
	var in struct {
		SceneIDs []int `json:"scene_ids"`
	}
	_ = json.Unmarshal(raw, &in)
	var out []string
	for _, id := range in.SceneIDs {
		sc, ok := s.scenes[id]
		if !ok {
			continue
		}
		var tags []string
		for i, n := range sc.tags {
			tags = append(tags, fmt.Sprintf(`{"id":"t%d","name":%q}`, i, n))
		}
		studio := "null"
		if sc.studio != "" {
			studio = fmt.Sprintf(`{"id":%q,"name":"Studio %s"}`, sc.studio, sc.studio)
		}
		out = append(out, fmt.Sprintf(`{"id":"%d","title":"Scene %d","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60}],"studio":%s,"tags":[%s]}`, id, id, studio, strings.Join(tags, ",")))
	}
	return json.Unmarshal([]byte(`{"findScenes":{"scenes":[`+strings.Join(out, ",")+`]}}`), resp.Data)
}

func (s *studioStash) queries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// studioEnv loads the default video rules with learning switched as on
// and returns a service over the scenes.
func studioEnv(t *testing.T, on bool, scenes map[int]studioScene) (*Service, *studioStash) {
	t.Helper()
	loadConfig(t, nil)
	cfg := config.Application()
	cfg.VideoRules = config.DefaultVideoRules()
	cfg.LearnStudioProfiles = on
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	st := &studioStash{scenes: scenes}
	return NewService(st), st
}

// saveAt stores a profile for id and sets its modification time.
func saveAt(t *testing.T, s *Service, id string, at time.Time) {
	t.Helper()
	if err := s.SaveProfile(id, []byte("profile "+id)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.ProfilePath(id), at, at); err != nil {
		t.Fatal(err)
	}
}

// learned is StudioProfile for scene id as the players resolve it.
func learned(t *testing.T, s *Service, id string) string {
	t.Helper()
	vd, err := s.GetScene(t.Context(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	f := ResolveFormat(config.Application().VideoRules, vd.SceneParts.Tags)
	return s.StudioProfile(t.Context(), vd, &f)
}

func TestLensKey(t *testing.T) {
	rules := config.DefaultVideoRules()
	for _, c := range []struct {
		tags []string
		want string
	}{
		{[]string{"MKX200"}, "fisheye|MKX200|200"},
		{[]string{"DOME"}, "equirectangular||0"},
		{[]string{"FISHEYE"}, "fisheye||0"},
	} {
		f := ResolveFormat(rules, tags(c.tags...))
		if got := LensKey(&f); got != c.want {
			t.Errorf("%v: got %q, want %q", c.tags, got, c.want)
		}
	}
}

var t0 = time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)

func TestStudioProfile_SameStudioAndLensOnly(t *testing.T) {
	s, _ := studioEnv(t, true, map[int]studioScene{
		1: {"a", []string{"MKX200"}},
		2: {"a", []string{"MKX200"}},
		3: {"a", []string{"DOME"}},
		4: {"b", []string{"MKX200"}},
		5: {"", []string{"MKX200"}},
		6: {"a", []string{"FISHEYE"}},
	})
	saveAt(t, s, "1", t0)

	for id, want := range map[string]string{
		"2": "1", // same studio, same lens
		"3": "",  // same studio, a 180 never inherits a fisheye profile
		"4": "",  // another studio
		"5": "",  // no studio
		"6": "",  // fisheye without the MKX200 lens and FOV
		"1": "",  // a scene never inherits its own profile
	} {
		if got := learned(t, s, id); got != want {
			t.Errorf("scene %s: got %q, want %q", id, got, want)
		}
	}
}

func TestStudioProfile_StudiolessProfilesAreNotLearned(t *testing.T) {
	s, _ := studioEnv(t, true, map[int]studioScene{
		1: {"", []string{"DOME"}},
		2: {"", []string{"DOME"}},
	})
	saveAt(t, s, "1", t0)

	if got := learned(t, s, "2"); got != "" {
		t.Fatalf("scenes without a studio never inherit, got %q", got)
	}
}

func TestStudioProfile_NewestSaveWins(t *testing.T) {
	s, st := studioEnv(t, true, map[int]studioScene{
		1: {"a", []string{"DOME"}},
		2: {"a", []string{"DOME"}},
		3: {"a", []string{"DOME"}},
	})
	saveAt(t, s, "1", t0.Add(time.Hour))
	saveAt(t, s, "2", t0)

	if got := learned(t, s, "3"); got != "1" {
		t.Fatalf("expected the newest saved profile, got %q", got)
	}

	// A new save refreshes the index without rebuilding it from Stash.
	if err := s.SaveProfile("2", []byte("tuned again")); err != nil {
		t.Fatal(err)
	}
	before := st.queries()
	if got := learned(t, s, "3"); got != "2" {
		t.Fatalf("expected the profile just saved, got %q", got)
	}
	if st.queries() != before {
		t.Fatalf("a save of a cached scene must not query Stash, got %d more queries", st.queries()-before)
	}
}

func TestStudioProfile_IndexIsBuiltOnce(t *testing.T) {
	s, st := studioEnv(t, true, map[int]studioScene{
		1: {"a", []string{"DOME"}},
		2: {"a", []string{"DOME"}},
		3: {"a", []string{"DOME"}},
		4: {"a", []string{"DOME"}},
	})
	saveAt(t, s, "1", t0)
	saveAt(t, s, "2", t0)
	for _, id := range []string{"3", "4"} {
		if _, err := s.GetScene(t.Context(), id, false); err != nil {
			t.Fatal(err)
		}
	}
	base := st.queries()

	learned(t, s, "3")
	if n := st.queries() - base; n != 1 {
		t.Fatalf("expected one batch query for the profile scenes, got %d", n)
	}
	learned(t, s, "3")
	learned(t, s, "4")
	if n := st.queries() - base; n != 1 {
		t.Fatalf("expected later lookups served from the index, got %d queries", n)
	}

	s.ResetCaches()
	learned(t, s, "3")
	if n := st.queries() - base; n < 2 {
		t.Fatal("expected ResetCaches to drop the index")
	}
}

func TestStudioProfile_RebuiltWhenTheRulesChange(t *testing.T) {
	s, _ := studioEnv(t, true, map[int]studioScene{
		1: {"a", []string{"Custom"}},
		2: {"a", []string{"DOME"}},
	})
	saveAt(t, s, "1", t0)
	if got := learned(t, s, "2"); got != "" {
		t.Fatalf("an untagged lens must not match a 180, got %q", got)
	}

	cfg := config.Application()
	cfg.VideoRules = append(config.DefaultVideoRules(), config.VideoRule{Tag: "Custom", Projection: "equirectangular"})
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := learned(t, s, "2"); got != "1" {
		t.Fatalf("expected the index rebuilt with the new rules, got %q", got)
	}
}

func TestStudioProfile_Off(t *testing.T) {
	s, st := studioEnv(t, false, map[int]studioScene{
		1: {"a", []string{"DOME"}},
		2: {"a", []string{"DOME"}},
	})
	saveAt(t, s, "1", t0)
	if _, err := s.GetScene(t.Context(), "2", false); err != nil {
		t.Fatal(err)
	}
	base := st.queries()

	if got := learned(t, s, "2"); got != "" {
		t.Fatalf("learning off must inherit nothing, got %q", got)
	}
	if st.queries() != base {
		t.Fatal("learning off must not build the index")
	}
}

func TestStudioProfile_StashDownInheritsNothing(t *testing.T) {
	s, _ := studioEnv(t, true, map[int]studioScene{2: {"a", []string{"DOME"}}})
	vd, err := s.GetScene(t.Context(), "2", false)
	if err != nil {
		t.Fatal(err)
	}
	saveAt(t, s, "1", t0)
	s.SetStashClient(failingClient{})
	f := ResolveFormat(config.Application().VideoRules, vd.SceneParts.Tags)

	if got := s.StudioProfile(t.Context(), vd, &f); got != "" {
		t.Fatalf("expected nothing while Stash is down, got %q", got)
	}
	if got := s.StudioProfile(t.Context(), nil, &f); got != "" {
		t.Fatalf("expected nothing for a nil scene, got %q", got)
	}
}

type failingClient struct{}

func (failingClient) MakeRequest(context.Context, *graphql.Request, *graphql.Response) error {
	return fmt.Errorf("stash unreachable")
}
