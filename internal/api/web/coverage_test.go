package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/stash/gql"
)

// coverageStash answers the coverage queries; every other query goes to
// fakeStash. The dimension query returns every scene regardless of the
// filter, so the aspect check in stash-vr is what is under test.
type coverageStash struct {
	fakeStash
	tagsErr  error
	tags     string // JSON array of {id,name,scene_count}
	scenes   string // JSON array of {id,title,files}
	lastScan *gql.SceneFilterType
}

func (f *coverageStash) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindTagSceneCounts":
		if f.tagsErr != nil {
			f.count(req.OpName)
			return f.tagsErr
		}
		payload = `{"findTags":{"tags":` + f.tags + `}}`
	case "FindSceneDimensions":
		raw, _ := json.Marshal(req.Variables)
		var in struct {
			SceneFilter *gql.SceneFilterType `json:"scene_filter"`
		}
		_ = json.Unmarshal(raw, &in)
		f.mu.Lock()
		f.lastScan = in.SceneFilter
		f.mu.Unlock()
		payload = `{"findScenes":{"count":0,"scenes":` + f.scenes + `}}`
	default:
		return f.fakeStash.MakeRequest(ctx, req, resp)
	}
	f.count(req.OpName)
	return json.Unmarshal([]byte(payload), resp.Data)
}

func (f *coverageStash) count(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[op]++
}

func (f *coverageStash) callCount(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func scene(id string, w, h int) string {
	return fmt.Sprintf(`{"id":%q,"title":"Scene %s","files":[{"width":%d,"height":%d}]}`, id, id, w, h)
}

func newCoverageEnv(t *testing.T) (*coverageStash, http.Handler) {
	t.Helper()
	stash := &coverageStash{
		tags: `[{"id":"10","name":"DOME","scene_count":12},{"id":"11","name":"FISHEYE","scene_count":3},` +
			`{"id":"12","name":"SBS","scene_count":40},{"id":"13","name":"VRP: Unresolved","scene_count":2},` +
			`{"id":"14","name":"cubemap","scene_count":1},{"id":"15","name":"Unrelated","scene_count":99}]`,
		scenes: `[` + strings.Join([]string{
			scene("1", 3840, 1920), // 2:1
			scene("2", 4096, 4096), // 1:1
			scene("3", 3840, 2160), // 16:9 flat 4K
			scene("4", 1920, 960),  // 2:1 but too small
			scene("5", 5760, 2880), // 2:1
			`{"id":"6","title":"No file","files":[]}`,
		}, ",") + `]`,
	}
	lib, h := newEnv(t, &stash.fakeStash)
	lib.SetStashClient(stash)
	return stash, h
}

type coverageOut struct {
	Tags []struct {
		Name    string `json:"name"`
		Count   int    `json:"count"`
		Link    string `json:"link"`
		Missing bool   `json:"missing"`
	} `json:"tags"`
	Untagged struct {
		Count  int `json:"count"`
		Scenes []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Stash  string `json:"stash"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"scenes"`
	} `json:"untagged"`
	CheckedAt string `json:"checked_at"`
}

func getCoverage(t *testing.T, h http.Handler) coverageOut {
	t.Helper()
	rec, _ := do(t, h, http.MethodGet, "/coverage", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out coverageOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// decodeCriterion reverses Stash's scene list encoding of one c= value.
func decodeCriterion(t *testing.T, link string) map[string]any {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/scenes" {
		t.Fatalf("path %q", u.Path)
	}
	c := u.Query()["c"]
	if len(c) != 1 {
		t.Fatalf("want one criterion in %q", link)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(translateCriterion(c[0], true)), &m); err != nil {
		t.Fatalf("criterion %q: %v", c[0], err)
	}
	return m
}

func TestCoverage_CountsManagedTagsInOrderWithLinks(t *testing.T) {
	_, h := newCoverageEnv(t)
	out := getCoverage(t, h)

	var names []string
	for _, tag := range out.Tags {
		names = append(names, tag.Name)
	}
	if strings.Join(names, ",") != strings.Join(coverageTags, ",") {
		t.Fatalf("tags %v", names)
	}
	byName := map[string]int{}
	for i, tag := range out.Tags {
		byName[tag.Name] = i
	}
	dome := out.Tags[byName["DOME"]]
	if dome.Count != 12 || dome.Missing {
		t.Fatalf("dome %+v", dome)
	}
	crit := decodeCriterion(t, dome.Link)
	value := crit["value"].(map[string]any)
	item := value["items"].([]any)[0].(map[string]any)
	if crit["type"] != "tags" || crit["modifier"] != "INCLUDES_ALL" || item["id"] != "10" || item["label"] != "DOME" || value["depth"] != 0.0 {
		t.Fatalf("criterion %v", crit)
	}
	unresolved := out.Tags[byName["VRP: Unresolved"]]
	if unresolved.Count != 2 || !strings.HasPrefix(unresolved.Link, "http://stash:9999/scenes?c=") {
		t.Fatalf("unresolved %+v", unresolved)
	}
	if decodeCriterion(t, unresolved.Link)["value"].(map[string]any)["items"].([]any)[0].(map[string]any)["label"] != "VRP: Unresolved" {
		t.Fatalf("label lost in %q", unresolved.Link)
	}
	rf52 := out.Tags[byName["RF52"]]
	if rf52.Count != 0 || !rf52.Missing || rf52.Link != "" {
		t.Fatalf("a tag Stash lacks has no link: %+v", rf52)
	}
	if out.CheckedAt == "" {
		t.Fatal("checked_at missing")
	}
}

func TestCoverage_UntaggedCountsOnlyVrShapedScenes(t *testing.T) {
	stash, h := newCoverageEnv(t)
	out := getCoverage(t, h)

	if out.Untagged.Count != 3 {
		t.Fatalf("untagged %+v", out.Untagged)
	}
	var ids []string
	for _, s := range out.Untagged.Scenes {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "1,2,5" {
		t.Fatalf("ids %v", ids)
	}
	if s := out.Untagged.Scenes[0]; s.Stash != "http://stash:9999/scenes/1" || s.Width != 3840 || s.Height != 1920 || s.Title != "Scene 1" {
		t.Fatalf("scene %+v", s)
	}

	f := stash.lastScan
	if f == nil || f.Resolution == nil || f.Resolution.Modifier != gql.CriterionModifierGreaterThan || f.Resolution.Value != gql.ResolutionEnumQuadHd {
		t.Fatalf("resolution criterion %+v", f)
	}
	if f.Tags == nil || f.Tags.Modifier != gql.CriterionModifierExcludes || strings.Join(f.Tags.Value, ",") != "10,11,14" {
		t.Fatalf("tag criterion %+v", f.Tags)
	}
}

func TestCoverage_NoProjectionTagsLeavesTagCriterionOut(t *testing.T) {
	stash, h := newCoverageEnv(t)
	stash.tags = `[{"id":"12","name":"SBS","scene_count":40}]`
	getCoverage(t, h)
	if stash.lastScan == nil || stash.lastScan.Tags != nil {
		t.Fatalf("filter %+v", stash.lastScan)
	}
}

func TestCoverage_CachedForAMinute(t *testing.T) {
	stash, h := newCoverageEnv(t)
	getCoverage(t, h)
	getCoverage(t, h)
	if n := stash.callCount("FindTagSceneCounts"); n != 1 {
		t.Fatalf("tag query ran %d times", n)
	}
	if n := stash.callCount("FindSceneDimensions"); n != 1 {
		t.Fatalf("scene query ran %d times", n)
	}
}

func TestCoverage_StashErrorIsBadGatewayAndNotCached(t *testing.T) {
	stash, h := newCoverageEnv(t)
	stash.tagsErr = errors.New("connection refused")
	rec, out := do(t, h, http.MethodGet, "/coverage", nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(fmt.Sprint(out["error"]), "connection refused") {
		t.Fatalf("status %d %v", rec.Code, out)
	}
	stash.tagsErr = nil
	getCoverage(t, h)
	if n := stash.callCount("FindTagSceneCounts"); n != 2 {
		t.Fatalf("a failure must not be cached, tag query ran %d times", n)
	}
}

func TestCoverageCache_ExpiresAndFollowsTheStashUrl(t *testing.T) {
	now := time.Unix(1000, 0)
	c := coverageCache{now: func() time.Time { return now }}
	calls := 0
	load := func() (*coverageReport, error) { calls++; return &coverageReport{}, nil }

	for range 2 {
		if _, err := c.get("a", load); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(coverageTTL - time.Second)
	_, _ = c.get("a", load)
	if calls != 1 {
		t.Fatalf("loaded %d times within the ttl", calls)
	}
	_, _ = c.get("b", load)
	if calls != 2 {
		t.Fatalf("another Stash must not reuse the result, loaded %d times", calls)
	}
	now = now.Add(coverageTTL + time.Second)
	_, _ = c.get("b", load)
	if calls != 3 {
		t.Fatalf("loaded %d times after expiry", calls)
	}
}

func TestStashTagListUrl_MatchesStashEncoding(t *testing.T) {
	got := StashTagListUrl("http://stash:9999/graphql", "5", "VRP: Skip")
	want := `http://stash:9999/scenes?c=(%22type%22:%22tags%22,%22modifier%22:%22INCLUDES_ALL%22,%22value%22:(%22items%22:%5B(%22id%22:%225%22,%22label%22:%22VRP:%20Skip%22)%5D,%22excluded%22:%5B%5D,%22depth%22:0))`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// Braces and reserved characters inside the label stay intact.
	odd := StashTagListUrl("https://h/stash/graphql", "7", `a{b}&c=d+e?"f"`)
	if !strings.HasPrefix(odd, "https://h/stash/scenes?c=") {
		t.Fatalf("base %q", odd)
	}
	label := decodeCriterion(t, strings.Replace(odd, "https://h/stash", "https://h", 1))["value"].(map[string]any)["items"].([]any)[0].(map[string]any)["label"]
	if label != `a{b}&c=d+e?"f"` {
		t.Fatalf("label %q from %s", label, odd)
	}
}

func TestIsVrShaped(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want bool
	}{
		{3840, 1920, true}, {4096, 2048, true}, {3840, 3840, true}, {8192, 4096, true},
		{3840, 2160, false}, {1920, 960, false}, {3840, 0, false}, {0, 0, false}, {3840, 1900, true},
	} {
		if got := isVrShaped(c.w, c.h); got != c.want {
			t.Errorf("%dx%d: got %v", c.w, c.h, got)
		}
	}
}
