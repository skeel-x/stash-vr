package hsp

import (
	"bytes"
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
// has no tags, 11 and 12 are Passthrough scenes of studio s1 (12 with a
// resume position of 42 s and a rating of 80), 5 fails,
// 20 is an SBS scene measured -0.62 degrees vertically off, 21 one
// measured 0.1, anything else does not exist.
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
		tags, studio, custom, extra := `[]`, `null`, `{}`, ``
		switch id {
		case 5:
			return errors.New("stash is down")
		case 7, 9:
			tags = passthrough
		case 11, 12:
			tags, studio = passthrough, `{"id":"s1","name":"Studio One"}`
			if id == 12 {
				extra = `,"resume_time":42,"rating100":80`
			}
		case 20, 21:
			tags = `[{"id":"2","name":"SBS","sort_name":"","aliases":[],"parents":[]}]`
			custom = `{"vr_vertical_offset":-0.62}`
			if id == 21 {
				custom = `{"vr_vertical_offset":"0.1"}`
			}
		case 8:
		default:
			continue
		}
		scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"Scene %d","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60,"path":"/s.mp4","height":1080}],"studio":%s,"tags":%s,"custom_fields":%s%s}`, id, id, studio, tags, custom, extra))
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

func setCorrectVertical(t *testing.T, on bool) {
	t.Helper()
	cfg := config.Application()
	cfg.CorrectVerticalStereo = on
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestHandler_GeneratesVerticalCorrection(t *testing.T) {
	lib, h := newRouter(t)
	setCorrectVertical(t, true)

	rec := get(h, "/hsp/scene/20")
	if rec.Code != 200 {
		t.Fatalf("corrected scene: got %d", rec.Code)
	}
	p, err := hspfile.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if p.Alignment[0].Rotation.Pitch != float32(-0.62) || p.Format[0].Stereo != hspfile.StereoSideBySide {
		t.Fatalf("expected pitch -0.62 on an SBS profile, got %+v %+v", p.Alignment[0], p.Format[0])
	}
	// Generation is deterministic.
	if again := get(h, "/hsp/scene/20"); !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) {
		t.Fatal("generated profile differs between requests")
	}

	// Too small to correct: nothing to serve.
	if rec := get(h, "/hsp/scene/21"); rec.Code != 404 {
		t.Fatalf("small offset: got %d", rec.Code)
	}

	// The scene's own profile still wins.
	if err := lib.SaveProfile("20", []byte("own")); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/hsp/scene/20"); rec.Body.String() != "own" {
		t.Fatalf("own: got %q", rec.Body.String())
	}
}

func TestHandler_VerticalCorrectionOff(t *testing.T) {
	_, h := newRouter(t)
	setCorrectVertical(t, false)
	if rec := get(h, "/hsp/scene/20"); rec.Code != 404 {
		t.Fatalf("setting off: got %d", rec.Code)
	}
}

// learnedProfile is a profile HereSphere would have saved for scene 11:
// tuned geometry, and 11's own name, position and tags.
func learnedProfile(t *testing.T) []byte {
	t.Helper()
	p := hspfile.Default()
	p.ID, p.Title = "http://example.com/heresphere/11", "Scene 11"
	p.Duration, p.Resume = hspfile.TimespanTicks(60), hspfile.TimespanTicks(31)
	p.ABStart, p.ABEnd = hspfile.TimespanTicks(5), hspfile.TimespanTicks(20)
	p.AverageRating = 2.5
	p.Tags = []hspfile.Tag{{Name: "Only on 11", End: p.Duration}}
	p.Alignment[0].Position.Y = 7.25
	p.Alignment[0].Rotation.Pitch = -1.5
	p.Format[0].Projection = hspfile.ProjectionFisheye
	p.Lens[0].TrueFOV, p.Lens[0].ExportFOV = 200, 200
	p.Environment[0].Background = hspfile.BackgroundPassthrough
	data, err := hspfile.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHandler_RebasesLearnedStudioProfileOnTheScene(t *testing.T) {
	lib, h := newRouter(t, geometryRules()...)
	if err := lib.SaveProfile("11", learnedProfile(t)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Application()
	cfg.LearnStudioProfiles = true
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}

	rec := get(h, "/hsp/scene/12")
	if rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
	checkHeaders(t, rec)
	p, err := hspfile.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	// Scene 11's geometry, every section of it.
	if p.Alignment[0].Position.Y != 7.25 || p.Alignment[0].Rotation.Pitch != -1.5 || p.Format[0].Projection != hspfile.ProjectionFisheye ||
		p.Lens[0].TrueFOV != 200 || p.Environment[0].Background != hspfile.BackgroundPassthrough {
		t.Fatalf("geometry not kept: %+v %+v %+v", p.Alignment[0], p.Format[0], p.Lens[0])
	}
	// Scene 12's own fields.
	if p.ID != "http://example.com/heresphere/12" || p.Title != "Scene 12" {
		t.Fatalf("id %q title %q", p.ID, p.Title)
	}
	if p.Resume != hspfile.TimespanTicks(42) || p.Duration != hspfile.TimespanTicks(60) {
		t.Fatalf("resume %d duration %d: must be scene 12's, not 11's", p.Resume, p.Duration)
	}
	if p.ABStart != 0 || p.ABEnd != 0 || p.AverageRating != 4 {
		t.Fatalf("ab %d-%d rating %v", p.ABStart, p.ABEnd, p.AverageRating)
	}
	if len(p.Tags) == 0 || p.Tags[0].Name == "Only on 11" {
		t.Fatalf("expected scene 12's tags, got %+v", p.Tags)
	}
	for _, tag := range p.Tags {
		if tag.Name == "Only on 11" {
			t.Fatalf("scene 11's tag leaked: %+v", p.Tags)
		}
	}

	// Scene 11 itself still gets its own profile untouched.
	if own := get(h, "/hsp/scene/11"); !bytes.Equal(own.Body.Bytes(), learnedProfile(t)) {
		t.Fatal("the owner's profile must be served as stored")
	}
}

func TestHandler_RebasesRuleProfileOnTheScene(t *testing.T) {
	lib, h := newRouter(t, geometryRules()...)
	if err := lib.SaveProfile("100", learnedProfile(t)); err != nil {
		t.Fatal(err)
	}

	rec := get(h, "/hsp/scene/7")
	if rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
	p, err := hspfile.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Scene 7" || p.ID != "http://example.com/heresphere/7" || p.Resume != 0 || p.Alignment[0].Position.Y != 7.25 {
		t.Fatalf("rebased profile %+v", p)
	}
}

func TestHandler_BorrowedProfileFallsBackToStoredBytes(t *testing.T) {
	lib, h := newRouter(t, geometryRules()...)
	// Not a profile at all: served as stored (the headset may still cope).
	if err := lib.SaveProfile("100", []byte("captured")); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/hsp/scene/7"); rec.Code != 200 || rec.Body.String() != "captured" {
		t.Fatalf("undecodable: got %d %q", rec.Code, rec.Body.String())
	}

	// A real profile, but nothing to rebase it with.
	if err := lib.SaveProfile("100", learnedProfile(t)); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Get("/none/{videoId}", Handler(lib, nil))
	r.Get("/fail/{videoId}", Handler(lib, func(*http.Request, *library.VideoData, library.Format) ([]byte, error) {
		return nil, errors.New("boom")
	}))
	r.Get("/junk/{videoId}", Handler(lib, func(*http.Request, *library.VideoData, library.Format) ([]byte, error) {
		return []byte("junk"), nil
	}))
	for _, path := range []string{"/none/7", "/fail/7", "/junk/7"} {
		rec := get(r, path)
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), learnedProfile(t)) {
			t.Fatalf("%s: expected the stored bytes, got %d (%d bytes)", path, rec.Code, rec.Body.Len())
		}
	}
}
