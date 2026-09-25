package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/static"
)

var previewSky = color.RGBA{R: 40, G: 90, B: 160, A: 255}

// previewScenes are the scenes previewStash knows: id to tag names and
// screenshot path.
var previewScenes = map[string]struct {
	tags []string
	shot string
}{
	"21": {[]string{"8K", "Alpha"}, "/shot"},
	"22": {[]string{"7K"}, "/shot"},
	"23": {nil, "/shot"},
	"24": {[]string{"8K"}, "/missing"},
}

// previewStash answers the queries behind the badge preview. tags maps a
// tag name to its id; scene filters pick scene 21 for a tier and Alpha
// filter, 22 for a tier filter alone and 23 for any scene.
type previewStash struct {
	base string
	tags map[string]string

	mu      sync.Mutex
	filters []string
}

func (s *previewStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	raw, _ := json.Marshal(req.Variables)
	payload := `{}`
	switch req.OpName {
	case "FindTagByName":
		var in struct{ Name string }
		_ = json.Unmarshal(raw, &in)
		if id, ok := s.tags[in.Name]; ok {
			payload = fmt.Sprintf(`{"findTags":{"tags":[{"id":%q}]}}`, id)
		} else {
			payload = `{"findTags":{"tags":[]}}`
		}
	case "FindSceneIdsByFilter":
		s.mu.Lock()
		s.filters = append(s.filters, string(raw))
		s.mu.Unlock()
		f := string(raw)
		switch {
		case strings.Contains(f, `"t8"`) && strings.Contains(f, `"ta"`):
			payload = `{"findScenes":{"scenes":[{"id":"21"}]}}`
		case strings.Contains(f, `"t8"`) || strings.Contains(f, `"t7"`):
			payload = `{"findScenes":{"scenes":[{"id":"22"}]}}`
		case !strings.Contains(f, `"tags"`) && strings.Contains(f, `"cover"`):
			payload = `{"findScenes":{"scenes":[{"id":"23"}]}}`
		default:
			payload = `{"findScenes":{"scenes":[]}}`
		}
	case "FindScenes":
		var in struct {
			SceneIDs []int `json:"scene_ids"`
		}
		_ = json.Unmarshal(raw, &in)
		var scenes []string
		for _, id := range in.SceneIDs {
			sc, ok := previewScenes[fmt.Sprint(id)]
			if !ok {
				continue
			}
			var tags []string
			for _, n := range sc.tags {
				tags = append(tags, fmt.Sprintf(`{"id":%q,"name":%q,"sort_name":"","aliases":[],"parents":[]}`, n, n))
			}
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"Scene %d / ÅÄÖ","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60,"path":"/s.mp4","width":8192,"height":4096,"video_codec":"hevc"}],"tags":[%s],"interactive":false,"paths":{"screenshot":"%s%s","stream":"%s/stream"}}`, id, id, strings.Join(tags, ","), s.base, sc.shot, s.base))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func (s *previewStash) sceneQueries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.filters)
}

// previewEnv serves a 400 x 200 sky screenshot and returns a handler
// over a previewStash with the given tags and saved badge settings.
func previewEnv(t *testing.T, tags map[string]string, saved config.CoverBadges) (*apiHandler, *previewStash) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.SetRGBA(x, y, previewSky)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/shot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("/missing", http.NotFound)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", LogLevel: "info",
		ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(), CoverBadges: saved,
	}); err != nil {
		t.Fatal(err)
	}
	coverbadge.ResetCache()
	t.Cleanup(coverbadge.ResetCache)
	st := &previewStash{base: srv.URL, tags: tags}
	return &apiHandler{lib: library.NewService(st)}, st
}

var allPreviewTags = map[string]string{"8K": "t8", "7K": "t7", "Alpha": "ta"}

func preview(h *apiHandler, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.badgePreview(rec, httptest.NewRequest(http.MethodGet, "/badge-preview?"+query, nil))
	return rec
}

func previewImage(t *testing.T, rec *httptest.ResponseRecorder) image.Image {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("not a jpeg: %v", err)
	}
	return img
}

// bottomLeftHas reports a pixel near c in the bottom left quarter of the
// 400 x 200 cover.
func bottomLeftHas(img image.Image, c color.RGBA) bool {
	for y := 150; y < 200; y++ {
		for x := 0; x < 200; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if absInt(int(r>>8)-int(c.R)) < 14 && absInt(int(g>>8)-int(c.G)) < 14 && absInt(int(b>>8)-int(c.B)) < 14 {
				return true
			}
		}
	}
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestBadgePreview_FlagsOverrideSettings(t *testing.T) {
	h, _ := previewEnv(t, allPreviewTags, config.CoverBadges{})

	img := previewImage(t, preview(h, "scene=21&quality=1&format=0&passthrough=1"))
	if !bottomLeftHas(img, coverbadge.Gold) || !bottomLeftHas(img, coverbadge.Accent) {
		t.Fatal("expected the 8K and AR badges although every badge is saved off")
	}

	h, _ = previewEnv(t, allPreviewTags, config.CoverBadges{Quality: true, Passthrough: true})
	img = previewImage(t, preview(h, "scene=21&quality=0&format=0&passthrough=0"))
	if bottomLeftHas(img, coverbadge.Gold) || bottomLeftHas(img, coverbadge.Accent) {
		t.Fatal("expected no badges although quality and passthrough are saved on")
	}
	if config.Application().CoverBadges != (config.CoverBadges{Quality: true, Passthrough: true}) {
		t.Fatal("a preview must not change the saved settings")
	}
}

func TestBadgePreview_HeadersAndSize(t *testing.T) {
	h, _ := previewEnv(t, allPreviewTags, config.CoverBadges{})

	rec := preview(h, "scene=21&quality=1&format=1&passthrough=1")

	img := previewImage(t, rec)
	if img.Bounds() != image.Rect(0, 0, 400, 200) {
		t.Fatalf("expected the cover size, got %v", img.Bounds())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store, got %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", got)
	}
	if got := rec.Header().Get("X-Scene-Id"); got != "21" {
		t.Fatalf("expected the scene id header, got %q", got)
	}
	title, err := url.PathUnescape(rec.Header().Get("X-Scene-Title"))
	if err != nil || title != "Scene 21 / ÅÄÖ" {
		t.Fatalf("expected the escaped scene title, got %q (%v)", rec.Header().Get("X-Scene-Title"), err)
	}
	if coverbadge.Rendered.Len() != 0 {
		t.Fatal("a preview must not land in the rendered cover cache")
	}
}

func TestBadgePreview_DefaultScene(t *testing.T) {
	for name, tc := range map[string]struct {
		tags map[string]string
		want string
	}{
		"tier and Alpha":  {allPreviewTags, "21"},
		"no Alpha tag":    {map[string]string{"8K": "t8", "7K": "t7"}, "22"},
		"no tier tag":     {map[string]string{"Alpha": "ta"}, "23"},
		"no managed tags": {map[string]string{}, "23"},
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := previewEnv(t, tc.tags, config.CoverBadges{})

			rec := preview(h, "quality=1&format=1&passthrough=1")

			previewImage(t, rec)
			if got := rec.Header().Get("X-Scene-Id"); got != tc.want {
				t.Fatalf("expected scene %s, got %q", tc.want, got)
			}
		})
	}
}

func TestBadgePreview_DefaultSceneFilters(t *testing.T) {
	h, st := previewEnv(t, allPreviewTags, config.CoverBadges{})

	previewImage(t, preview(h, "quality=1"))

	if len(st.filters) != 1 {
		t.Fatalf("expected one scene query, got %d", len(st.filters))
	}
	f := st.filters[0]
	// The first query pairs the top tier with Alpha in one INCLUDES_ALL
	// criterion: Stash ignores a second tags criterion nested under AND.
	for _, want := range []string{`"modifier":"INCLUDES_ALL"`, `"value":["t8","ta"]`, `"is_missing":"cover"`, `"per_page":1`} {
		if !strings.Contains(f, want) {
			t.Errorf("scene filter %s lacks %s", f, want)
		}
	}
	if strings.Contains(f, `"AND"`) {
		t.Errorf("scene filter %s must not nest tags under AND", f)
	}
}

func TestBadgePreview_DefaultSceneIsCachedForTenMinutes(t *testing.T) {
	h, st := previewEnv(t, allPreviewTags, config.CoverBadges{})
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	h.previewScene.now = func() time.Time { return now }

	previewImage(t, preview(h, "quality=1"))
	previewImage(t, preview(h, "quality=0"))
	if n := st.sceneQueries(); n != 1 {
		t.Fatalf("expected the default scene reused, got %d scene queries", n)
	}

	now = now.Add(10*time.Minute + time.Second)
	previewImage(t, preview(h, "quality=1"))
	if n := st.sceneQueries(); n != 2 {
		t.Fatalf("expected a fresh pick after ten minutes, got %d scene queries", n)
	}
}

func TestBadgePreview_NoSceneAtAll(t *testing.T) {
	h, _ := previewEnv(t, map[string]string{}, config.CoverBadges{})
	h.lib = library.NewService(&emptyStash{})

	rec := preview(h, "quality=1")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 with no scene to show, got %d", rec.Code)
	}
}

// emptyStash has no tags and no scenes.
type emptyStash struct{}

func (emptyStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	payload := `{"findTags":{"tags":[]}}`
	if req.OpName != "FindTagByName" {
		payload = `{"findScenes":{"scenes":[]}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

// failingStash fails every request.
type failingStash struct{}

func (failingStash) MakeRequest(context.Context, *graphql.Request, *graphql.Response) error {
	return fmt.Errorf("stash unreachable")
}

func TestBadgePreview_Errors(t *testing.T) {
	h, _ := previewEnv(t, allPreviewTags, config.CoverBadges{})
	for query, want := range map[string]int{
		"scene=99&quality=1":    http.StatusNotFound,
		"scene=24&quality=1":    http.StatusNotFound,
		"scene=abc&quality=1":   http.StatusBadRequest,
		"scene=21&quality=yes!": http.StatusBadRequest,
	} {
		rec := preview(h, query)
		if rec.Code != want {
			t.Errorf("%s: expected %d, got %d", query, want, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: expected no-store on errors, got %q", query, got)
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: expected a JSON error, got %q", query, rec.Header().Get("Content-Type"))
		}
	}

	h.lib = library.NewService(failingStash{})
	for _, query := range []string{"scene=21", ""} {
		if rec := preview(h, query); rec.Code != http.StatusBadGateway {
			t.Errorf("%q with Stash down: expected 502, got %d", query, rec.Code)
		}
	}
}

func TestBadgePreview_Routed(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, _ := do(t, h, http.MethodGet, "/badge-preview?scene=abc", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected the route to answer 400 for a bad id, got %d", rec.Code)
	}
}

func TestSetup_RendersBadgePreview(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})

	body := getPage(t, PagesRouter(lib), "/setup", nil).Body.String()

	ids := []string{`id="badge-preview"`, `id="badge-preview-img"`, `id="badge-preview-title"`, `id="badge-preview-msg"`, `id="badge-preview-scene"`, `id="badge-preview-show"`}
	for _, want := range ids {
		if !strings.Contains(body, want) {
			t.Errorf("setup missing %s", want)
		}
	}
	passthrough := strings.Index(body, `name="cover_badge_passthrough"`)
	block := strings.Index(body, `id="badge-preview"`)
	rules := strings.Index(body, "<h2>Video rules</h2>")
	if block < passthrough || block > rules {
		t.Fatal("the preview belongs under the cover badge checkboxes")
	}

	js, err := fs.ReadFile(static.Fs, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#badge-preview-img", "#badge-preview-scene", "#badge-preview-show", "/badge-preview?", "X-Scene-Title"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("app.js does not wire %s", want)
		}
	}
}
