package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
	"stash-vr/internal/static"
)

// inspectStash answers FindScenes with the requested scenes that exist,
// each with its own tags; every other query goes to fakeStash.
type inspectStash struct {
	fakeStash
	findErr error
	scenes  map[int]string // id -> title and tags as JSON fields
}

func (f *inspectStash) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	if req.OpName != "FindScenes" {
		return f.fakeStash.MakeRequest(ctx, req, resp)
	}
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls["FindScenes"]++
	f.mu.Unlock()
	if f.findErr != nil {
		return f.findErr
	}
	raw, _ := json.Marshal(req.Variables)
	var in struct {
		SceneIDs []int `json:"scene_ids"`
	}
	_ = json.Unmarshal(raw, &in)
	var out []string
	for _, id := range in.SceneIDs {
		if fields, ok := f.scenes[id]; ok {
			out = append(out, fmt.Sprintf(`{"id":"%d",%s,"created_at":"2024-01-01T00:00:00Z","files":[{"basename":"x.mp4","duration":60}]}`, id, fields))
		}
	}
	return json.Unmarshal([]byte(`{"findScenes":{"scenes":[`+strings.Join(out, ",")+`]}}`), resp.Data)
}

func tagsJson(names ...string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf(`{"id":"%d","name":%q}`, i+1, n)
	}
	return `"tags":[` + strings.Join(parts, ",") + `]`
}

func newInspectEnv(t *testing.T) (*inspectStash, http.Handler, func(string) (int, map[string]any)) {
	t.Helper()
	stash := &inspectStash{scenes: map[int]string{
		11649: `"title":"ARPorn - Hush-Hush Hottie",` + tagsJson("FISHEYE", "SBS", "Alpha"),
		2:     `"title":"Another dome",` + tagsJson("DOME", "RL"),
	}}
	lib, h := newEnv(t, &stash.fakeStash)
	lib.SetStashClient(stash)
	get := func(q string) (int, map[string]any) {
		rec, out := do(t, h, http.MethodGet, "/inspect?q="+q, nil)
		return rec.Code, out
	}
	return stash, h, get
}

func scenesOf(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, ok := out["scenes"].([]any)
	if !ok {
		t.Fatalf("no scenes list in %v", out)
	}
	scenes := make([]map[string]any, len(raw))
	for i, s := range raw {
		scenes[i] = s.(map[string]any)
	}
	return scenes
}

func TestInspect_ById(t *testing.T) {
	_, _, get := newInspectEnv(t)
	code, out := get("11649")
	if code != 200 {
		t.Fatalf("status %d %v", code, out)
	}
	scenes := scenesOf(t, out)
	if len(scenes) != 1 {
		t.Fatalf("expected one scene, got %v", scenes)
	}
	s := scenes[0]
	if s["id"] != "11649" || s["title"] != "ARPorn - Hush-Hush Hottie" || s["stash"] != "http://stash:9999/scenes/11649" {
		t.Fatalf("scene %v", s)
	}
	rules := config.Application().VideoRules
	var want []string
	for i := range rules {
		if rules[i].Tag == "FISHEYE" || rules[i].Tag == "SBS" || rules[i].Tag == "Alpha" {
			want = append(want, fmt.Sprintf("%d:%s", i, rules[i].Tag))
		}
	}
	var got []string
	for _, m := range s["matched"].([]any) {
		mm := m.(map[string]any)
		got = append(got, fmt.Sprintf("%v:%v", mm["index"], mm["tag"]))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matched %v want %v", got, want)
	}
	f := s["format"].(map[string]any)
	if f["projection"] != "fisheye" || f["stereo"] != "sbs" || f["passthrough"] != true || f["generated"] != nil {
		t.Fatalf("format %v", f)
	}
	p := s["profile"].(map[string]any)
	if p["source"] != "none" || p["link"] != nil {
		t.Fatalf("profile %v", p)
	}
}

func TestInspect_ProfileSources(t *testing.T) {
	_, _, get := newInspectEnv(t)
	// Scene 2 has RL, which swaps eyes in a generated profile.
	_, out := get("2")
	s := scenesOf(t, out)[0]
	if f := s["format"].(map[string]any); f["eye_swap"] != true || f["generated"] != true || f["projection"] != "equirectangular" {
		t.Fatalf("format %v", f)
	}
	if p := s["profile"].(map[string]any); p["source"] != "generated" || p["scene"] != "2" || p["link"] != "http://example.com/hsp/scene/2" {
		t.Fatalf("profile %v", p)
	}
}

func TestInspect_OwnAndRuleProfile(t *testing.T) {
	stash := &inspectStash{scenes: map[int]string{
		11649: `"title":"Hush",` + tagsJson("Alpha"),
		5:     `"title":"Five",` + tagsJson("Alpha"),
	}}
	lib, h := newEnv(t, &stash.fakeStash)
	lib.SetStashClient(stash)
	if err := lib.SaveProfile("11649", []byte("x")); err != nil {
		t.Fatal(err)
	}
	y := 4.78
	cfg := config.Application()
	cfg.VideoRules = []config.VideoRule{{Tag: "Alpha", Profile: "11649", PositionY: &y}}
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	_, out := do(t, h, http.MethodGet, "/inspect?q=11649", nil)
	if p := scenesOf(t, out)[0]["profile"].(map[string]any); p["source"] != "own" || p["link"] != "http://example.com/hsp/scene/11649" {
		t.Fatalf("own: %v", p)
	}
	_, out = do(t, h, http.MethodGet, "/inspect?q=5", nil)
	s := scenesOf(t, out)[0]
	if p := s["profile"].(map[string]any); p["source"] != "rule" || p["scene"] != "11649" || p["link"] != "http://example.com/hsp/scene/11649" {
		t.Fatalf("rule: %v", p)
	}
	f := s["format"].(map[string]any)
	if g, ok := f["geometry"].(map[string]any); !ok || g["position_y"] != 4.78 || f["profile_scene"] != "11649" {
		t.Fatalf("format %v", f)
	}
}

func TestInspect_ByTitleUsesTheCache(t *testing.T) {
	stash, _, get := newInspectEnv(t)
	code, out := get("hush")
	if code != 200 || len(scenesOf(t, out)) != 0 {
		t.Fatalf("nothing cached yet must find nothing: %d %v", code, out)
	}
	if _, out := get("11649"); len(scenesOf(t, out)) != 1 {
		t.Fatal("id lookup failed")
	}
	stash.mu.Lock()
	before := stash.calls["FindScenes"]
	stash.mu.Unlock()
	_, out = get("HUSH-hush")
	scenes := scenesOf(t, out)
	if len(scenes) != 1 || scenes[0]["id"] != "11649" {
		t.Fatalf("title search: %v", scenes)
	}
	stash.mu.Lock()
	after := stash.calls["FindScenes"]
	stash.mu.Unlock()
	if after != before {
		t.Fatalf("a title search must not query Stash, FindScenes went %d -> %d", before, after)
	}
}

func TestInspect_ErrorsAndUnknown(t *testing.T) {
	stash, _, get := newInspectEnv(t)
	if code, _ := get(""); code != http.StatusBadRequest {
		t.Fatalf("empty query: %d", code)
	}
	if code, out := get("999"); code != 200 || len(scenesOf(t, out)) != 0 {
		t.Fatalf("unknown id: %d %v", code, out)
	}
	stash.findErr = errors.New("stash down")
	if code, _ := get("11649"); code != http.StatusBadGateway {
		t.Fatalf("stash error: %d", code)
	}
}

func TestInspect_StudioProfile(t *testing.T) {
	stash := &inspectStash{scenes: map[int]string{
		30: `"title":"Tuned","studio":{"id":"s1","name":"One"},` + tagsJson("MKX200"),
		31: `"title":"Same lens","studio":{"id":"s1","name":"One"},` + tagsJson("MKX200"),
		32: `"title":"Other lens","studio":{"id":"s1","name":"One"},` + tagsJson("DOME"),
	}}
	lib, h := newEnv(t, &stash.fakeStash)
	lib.SetStashClient(stash)
	if err := lib.SaveProfile("30", []byte("x")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Application()
	cfg.VideoRules = config.DefaultVideoRules()
	cfg.LearnStudioProfiles = true
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}

	_, out := do(t, h, http.MethodGet, "/inspect?q=31", nil)
	if p := scenesOf(t, out)[0]["profile"].(map[string]any); p["source"] != "studio" || p["scene"] != "30" || p["link"] != "http://example.com/hsp/scene/30" {
		t.Fatalf("studio: %v", p)
	}
	_, out = do(t, h, http.MethodGet, "/inspect?q=32", nil)
	if p := scenesOf(t, out)[0]["profile"].(map[string]any); p["source"] == "studio" {
		t.Fatalf("another lens must not inherit: %v", p)
	}

	js, err := fs.ReadFile(static.Fs, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), "'studio (from scene ' + s.profile.scene + ')'") {
		t.Fatal("app.js must label a learned profile as studio (from scene N)")
	}
}
