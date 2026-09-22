package heatmap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// imageServer serves fixtures the way Stash serves screenshots.
func imageServer(t *testing.T) (*httptest.Server, []byte, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 90, B: 40, A: 255})
		}
	}
	var jpg, pn bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pn, img); err != nil {
		t.Fatal(err)
	}
	webp, err := os.ReadFile("testdata/cover.webp")
	if err != nil {
		t.Fatal(err)
	}
	heatmap, err := os.ReadFile("testdata/heatmap.png")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/jpeg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpg.Bytes())
	})
	mux.HandleFunc("/png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pn.Bytes())
	})
	mux.HandleFunc("/webp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(webp)
	})
	mux.HandleFunc("/heatmap", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(heatmap)
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, jpg.Bytes(), pn.Bytes()
}

// sceneStash answers FindScenes with one scene per requested id, whose
// screenshot path is chosen by id.
type sceneStash struct {
	base string
}

func (s *sceneStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	if req.OpName != "FindScenes" {
		return json.Unmarshal([]byte(`{}`), resp.Data)
	}
	raw, _ := json.Marshal(req.Variables)
	var in struct {
		SceneIDs []int `json:"scene_ids"`
	}
	_ = json.Unmarshal(raw, &in)
	var scenes []string
	for _, id := range in.SceneIDs {
		shot := map[int]string{1: "/jpeg", 2: "/webp", 3: "/png", 4: "/missing", 5: "/jpeg", 6: "/broken"}[id]
		interactive := id == 5
		screenshotURL := s.base + shot
		if id == 7 {
			// A closed port: the transport error carries the keyed URL.
			screenshotURL = "http://127.0.0.1:1/x"
		}
		scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"S%d","created_at":"2024-01-01T00:00:00Z","files":[],"tags":[],"interactive":%t,"paths":{"screenshot":"%s","interactive_heatmap":"%s/heatmap","stream":"%s/stream","preview":"","funscript":"","caption":""}}`, id, id, interactive, screenshotURL, s.base, s.base))
	}
	payload := `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	return json.Unmarshal([]byte(payload), resp.Data)
}

func coverRouter(t *testing.T, base string) http.Handler {
	t.Helper()
	lib := library.NewService(&sceneStash{base: base})
	r := chi.NewRouter()
	r.Get("/cover/{videoId}", CoverHandler(lib))
	return r
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestCover_JpegPassesThroughUnchanged(t *testing.T) {
	srv, jpg, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/1")

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), jpg) {
		t.Fatal("jpeg body must pass through byte-identical")
	}
	if rec.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatalf("missing cache header, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCover_PngPassesThroughUnchanged(t *testing.T) {
	srv, _, pn := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/3")

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pn) {
		t.Fatalf("png must pass through, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestCover_WebpBecomesJpeg(t *testing.T) {
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/2")

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if _, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("body is not a jpeg: %v", err)
	}
}

func TestCover_MissingScreenshotIs404(t *testing.T) {
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	if rec := get(t, h, "/cover/4"); rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCover_MissingScreenshotIsNotCached(t *testing.T) {
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/4")

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store for a miss, got %q", got)
	}
}

func TestCover_UpstreamErrorIs502NoStore(t *testing.T) {
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/6")

	if rec.Code != 502 {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store for an upstream error, got %q", got)
	}
}

func TestCover_TransportErrorDoesNotLogTheKey(t *testing.T) {
	if err := config.Load(config.ApplicationConfig{
		ListenAddress:    ":9666",
		StashGraphQLUrl:  "http://stash:9999/graphql",
		StashApiKey:      "secret",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		ConfigPath:       t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	var buf bytes.Buffer
	bufLogger := zerolog.New(&buf)
	prevLogger := log.Logger
	prevCtxLogger := zerolog.DefaultContextLogger
	log.Logger = bufLogger
	zerolog.DefaultContextLogger = &bufLogger
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.DefaultContextLogger = prevCtxLogger
	})

	rec := get(t, h, "/cover/7")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if strings.Contains(buf.String(), "secret") {
		t.Fatalf("log leaked the api key: %s", buf.String())
	}
}

func TestCover_InteractiveSceneStillGetsHeatmapJpeg(t *testing.T) {
	srv, jpg, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/5")

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if bytes.Equal(rec.Body.Bytes(), jpg) {
		t.Fatal("interactive cover must be re-encoded with the heatmap, not passed through")
	}
}
