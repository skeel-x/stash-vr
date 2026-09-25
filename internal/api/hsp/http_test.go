package hsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"

	"stash-vr/internal/api/heresphere"
	"stash-vr/internal/config"
	hspfile "stash-vr/internal/hsp"
	"stash-vr/internal/library"
)

// fakeStash answers FindScenes: scene 7 and 9 are tagged Passthrough, 8
// has no tags, 11 and 12 are Passthrough scenes of studio s1, 5 fails,
// anything else does not exist.
type fakeStash struct{}

func (fakeStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var vars struct {
		Ids []int `json:"scene_ids"`
	}
	b, _ := json.Marshal(req.Variables)
	_ = json.Unmarshal(b, &vars)
	if req.OpName != "FindScenes" {
		return json.Unmarshal([]byte(`{}`), resp.Data)
	}
	const passthrough = `[{"id":"1","name":"Passthrough","sort_name":"","aliases":[],"parents":[]}]`
	var scenes []string
	for _, id := range vars.Ids {
		tags, studio := `[]`, `null`
		switch id {
		case 5:
			return errors.New("stash is down")
		case 7, 9:
			tags = passthrough
		case 11, 12:
			tags, studio = passthrough, `{"id":"s1","name":"Studio One"}`
		case 8:
		default:
			continue
		}
		scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"Scene %d","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60,"path":"/s.mp4","height":1080}],"studio":%s,"tags":%s}`, id, id, studio, tags))
	}
	return json.Unmarshal([]byte(`{"findScenes":{"scenes":[`+strings.Join(scenes, ",")+`]}}`), resp.Data)
}

func newRouter(t *testing.T, rules ...config.VideoRule) (*library.Service, http.Handler) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", FavoriteTag: "FAVORITE",
		LogLevel: "info", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if rules != nil {
		cfg := config.Application()
		cfg.VideoRules = rules
		if _, err := config.Set(cfg); err != nil {
			t.Fatal(err)
		}
	}
	lib := library.NewService(fakeStash{})
	r := chi.NewRouter()
	r.Get("/hsp/scene/{videoId}", Handler(lib, heresphere.GenerateProfile))
	return lib, r
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func checkHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache control %q", cc)
	}
}

func TestHandler_ServesStoredProfile(t *testing.T) {
	lib, h := newRouter(t)
	if err := lib.SaveProfile("7", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	rec := get(h, "/hsp/scene/7")

	if rec.Code != 200 || rec.Body.String() != "\x01\x02\x03" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	checkHeaders(t, rec)
}

func TestHandler_NotFound(t *testing.T) {
	_, h := newRouter(t)
	for _, p := range []string{"/hsp/scene/8", "/hsp/scene/7", "/hsp/scene/404", "/hsp/scene/abc", "/hsp/scene/..%2Fconfig.json"} {
		if rec := get(h, p); rec.Code != 404 {
			t.Errorf("%s: expected 404, got %d", p, rec.Code)
		}
	}
}

func TestHandler_StashErrorIsBadGateway(t *testing.T) {
	_, h := newRouter(t)
	if rec := get(h, "/hsp/scene/5"); rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

func geometryRules() []config.VideoRule {
	y := 4.78
	return []config.VideoRule{{Tag: "Passthrough", Passthrough: true, Profile: "100", PositionY: &y}}
}

func TestHandler_Precedence(t *testing.T) {
	lib, h := newRouter(t, geometryRules()...)

	// Nothing stored: generated from the rule.
	rec := get(h, "/hsp/scene/7")
	if rec.Code != 200 {
		t.Fatalf("generated: got %d", rec.Code)
	}
	checkHeaders(t, rec)
	p, err := hspfile.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Scene 7" || p.Alignment[0].Position.Y != float32(4.78) || p.Environment[0].Background != hspfile.BackgroundPassthrough || p.Environment[0].Mask != hspfile.MaskAlphaPacked {
		t.Fatalf("generated profile %+v", p)
	}
	if p.ID != "http://example.com/heresphere/7" {
		t.Fatalf("id %q", p.ID)
	}
	if len(p.Tags) == 0 {
		t.Fatal("expected the scene's tags")
	}

	// The rule's captured profile beats generation.
	if err := lib.SaveProfile("100", []byte("captured")); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/hsp/scene/7"); rec.Code != 200 || rec.Body.String() != "captured" {
		t.Fatalf("captured: got %d %q", rec.Code, rec.Body.String())
	}

	// The scene's own profile beats both.
	if err := lib.SaveProfile("7", []byte("own")); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/hsp/scene/7"); rec.Code != 200 || rec.Body.String() != "own" {
		t.Fatalf("own: got %d %q", rec.Code, rec.Body.String())
	}

	// A scene the rule does not match gets nothing.
	if rec := get(h, "/hsp/scene/8"); rec.Code != 404 {
		t.Fatalf("untagged scene: got %d", rec.Code)
	}
}

func TestHandler_GeneratorFailureAndNilGenerator(t *testing.T) {
	_, _ = newRouter(t, geometryRules()...)
	lib := library.NewService(fakeStash{})
	r := chi.NewRouter()
	r.Get("/fail/{videoId}", Handler(lib, func(*http.Request, *library.VideoData, library.Format) ([]byte, error) {
		return nil, errors.New("boom")
	}))
	r.Get("/none/{videoId}", Handler(lib, nil))
	if rec := get(r, "/fail/9"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing generator: got %d", rec.Code)
	}
	if rec := get(r, "/none/9"); rec.Code != 404 {
		t.Fatalf("no generator: got %d", rec.Code)
	}
}

func TestHandler_ServesLearnedStudioProfile(t *testing.T) {
	lib, h := newRouter(t, geometryRules()...)
	if err := lib.SaveProfile("100", []byte("rule profile")); err != nil {
		t.Fatal(err)
	}
	if err := lib.SaveProfile("11", []byte("studio profile")); err != nil {
		t.Fatal(err)
	}

	if rec := get(h, "/hsp/scene/12"); rec.Code != 200 || rec.Body.String() != "rule profile" {
		t.Fatalf("learning off: expected the rule's profile, got %d %q", rec.Code, rec.Body.String())
	}

	cfg := config.Application()
	cfg.LearnStudioProfiles = true
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/hsp/scene/12")
	if rec.Code != 200 || rec.Body.String() != "studio profile" {
		t.Fatalf("expected the studio profile of scene 11, got %d %q", rec.Code, rec.Body.String())
	}
	checkHeaders(t, rec)
	if rec := get(h, "/hsp/scene/9"); rec.Body.String() != "rule profile" {
		t.Fatalf("a scene without a studio keeps the rule's profile, got %q", rec.Body.String())
	}
}
