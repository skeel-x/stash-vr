package playa

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
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

var posterSky = color.RGBA{R: 40, G: 90, B: 160, A: 255}

// posterShots maps a scene id to the path its screenshot is served on:
// 1 a JPEG, 2 a PNG, 3 a WebP, 4 missing, 5 a Stash error, 6 and 7 the
// JPEG of interactive scenes whose heatmap posterHeatmaps gives.
var posterShots = map[string]string{"1": "/shot", "2": "/png", "3": "/webp", "4": "/missing", "5": "/broken", "6": "/shot", "7": "/shot"}

// posterHeatmaps maps an interactive scene id to its heatmap path: 6 one
// Stash fails to serve, 7 one it serves.
var posterHeatmaps = map[string]string{"6": "/broken", "7": "/heatmap"}

// posterStash answers FindScenes with the requested 8K scene whose
// screenshot is served by base on the path posterShots gives its id.
type posterStash struct{ base string }

func (s *posterStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	payload := `{}`
	if req.OpName == "FindScenes" {
		raw, _ := json.Marshal(req.Variables)
		var in struct {
			SceneIDs []int `json:"scene_ids"`
		}
		_ = json.Unmarshal(raw, &in)
		var scenes []string
		for _, id := range in.SceneIDs {
			shot, ok := posterShots[fmt.Sprint(id)]
			if !ok {
				continue
			}
			heat, interactive := posterHeatmaps[fmt.Sprint(id)]
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"One","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60,"path":"/s.mp4","width":8192,"height":4096,"video_codec":"hevc"}],"tags":[{"id":"8","name":"8K","sort_name":"","aliases":[],"parents":[]}],"interactive":%t,"paths":{"screenshot":"%s%s","interactive_heatmap":"%s%s","stream":"%s/stream"}}`, id, interactive, s.base, shot, s.base, heat, s.base))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

// posterFixtures are the bytes the fixture server serves as JPEG and PNG,
// set by posterEnv.
var posterFixtures struct{ jpg, png []byte }

func posterEnv(t *testing.T, badges config.CoverBadges) httpHandler {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.SetRGBA(x, y, posterSky)
		}
	}
	var jpg, pn bytes.Buffer
	if err := jpeg.Encode(&jpg, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pn, img); err != nil {
		t.Fatal(err)
	}
	posterFixtures.jpg, posterFixtures.png = jpg.Bytes(), pn.Bytes()
	webp, err := os.ReadFile(filepath.Join("..", "heatmap", "testdata", "cover.webp"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/shot", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	mux.HandleFunc("/heatmap", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("..", "heatmap", "testdata", "heatmap.png"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", LogLevel: "info",
		ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(), CoverBadges: badges,
	}); err != nil {
		t.Fatal(err)
	}
	coverbadge.ResetCache()
	t.Cleanup(coverbadge.ResetCache)
	return httpHandler{libraryService: library.NewService(&posterStash{base: srv.URL})}
}

// goldCorner reports a gold badge in the bottom left corner of the 400 x
// 200 poster.
func goldCorner(img image.Image) bool {
	for y := 150; y < 200; y++ {
		for x := 0; x < 100; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			gold := coverbadge.Gold
			if absDiff(r>>8, gold.R) < 12 && absDiff(g>>8, gold.G) < 12 && absDiff(b>>8, gold.B) < 12 {
				return true
			}
		}
	}
	return false
}

func absDiff(a uint32, b uint8) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}

func TestPosterHandler_DrawsBadges(t *testing.T) {
	h := posterEnv(t, config.CoverBadges{Quality: true})
	w := httptest.NewRecorder()

	h.posterHandler(w, newPlayaRequestWithVideoId("1"))

	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	img, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !goldCorner(img) {
		t.Fatal("expected the gold 8K badge on the poster")
	}
	if coverbadge.Rendered.Len() != 1 {
		t.Fatal("expected the poster in the shared rendered cover cache")
	}
}

func TestPosterHandler_WithoutBadges(t *testing.T) {
	h := posterEnv(t, config.CoverBadges{})
	w := httptest.NewRecorder()

	h.posterHandler(w, newPlayaRequestWithVideoId("1"))

	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if !bytes.Equal(w.Body.Bytes(), posterFixtures.jpg) {
		t.Fatal("a plain JPEG poster must pass through byte-identical, not be re-encoded")
	}
	if coverbadge.Rendered.Len() != 0 {
		t.Fatal("a pass-through poster must not be cached")
	}
}

func TestPosterHandler_PassesThroughAndTranscodesLikeCover(t *testing.T) {
	for name, c := range map[string]struct {
		id          string
		contentType string
	}{
		"png passes through":     {"2", "image/png"},
		"webp is transcoded":     {"3", "image/jpeg"},
		"jpeg is byte-identical": {"1", "image/jpeg"},
	} {
		t.Run(name, func(t *testing.T) {
			h := posterEnv(t, config.CoverBadges{})
			w := httptest.NewRecorder()

			h.posterHandler(w, newPlayaRequestWithVideoId(c.id))

			if w.Code != 200 || w.Header().Get("Content-Type") != c.contentType {
				t.Fatalf("got %d %q, want 200 %q", w.Code, w.Header().Get("Content-Type"), c.contentType)
			}
			if w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
				t.Fatalf("Content-Length %q does not match the %d byte body", w.Header().Get("Content-Length"), w.Body.Len())
			}
			switch c.id {
			case "1":
				if !bytes.Equal(w.Body.Bytes(), posterFixtures.jpg) {
					t.Fatal("expected the JPEG as Stash serves it")
				}
			case "2":
				if !bytes.Equal(w.Body.Bytes(), posterFixtures.png) {
					t.Fatal("expected the PNG as Stash serves it")
				}
			case "3":
				if _, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
					t.Fatalf("expected a JPEG, got %v", err)
				}
			}
		})
	}
}

func TestPosterHandler_HeatmapFetchFailureIsServedUncachedWithAWarning(t *testing.T) {
	h := posterEnv(t, config.CoverBadges{})
	var logs bytes.Buffer
	bufLogger := zerolog.New(&logs)
	prevLogger, prevCtxLogger := log.Logger, zerolog.DefaultContextLogger
	log.Logger, zerolog.DefaultContextLogger = bufLogger, &bufLogger
	t.Cleanup(func() { log.Logger, zerolog.DefaultContextLogger = prevLogger, prevCtxLogger })
	w := httptest.NewRecorder()

	h.posterHandler(w, newPlayaRequestWithVideoId("6"))

	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("a poster rendered without its heatmap must not be kept, got %q", got)
	}
	if !strings.Contains(logs.String(), `"level":"warn"`) || !strings.Contains(logs.String(), "Heatmap unavailable") {
		t.Fatalf("expected a warning about the heatmap, got %s", logs.String())
	}
}

func TestPosterHandler_HeatmapPosterIsKept(t *testing.T) {
	h := posterEnv(t, config.CoverBadges{})
	w := httptest.NewRecorder()

	h.posterHandler(w, newPlayaRequestWithVideoId("7"))

	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Fatalf("got %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if bytes.Equal(w.Body.Bytes(), posterFixtures.jpg) || coverbadge.Rendered.Len() != 1 {
		t.Fatal("expected the poster rendered with its heatmap and cached")
	}
}

func TestPosterHandler_ScreenshotFailures(t *testing.T) {
	for name, c := range map[string]struct {
		id   string
		code int
	}{
		"missing screenshot is 404": {"4", http.StatusNotFound},
		"stash error is 500":        {"5", http.StatusInternalServerError},
		"unknown scene is 404":      {"9", http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			h := posterEnv(t, config.CoverBadges{})
			w := httptest.NewRecorder()

			h.posterHandler(w, newPlayaRequestWithVideoId(c.id))

			if w.Code != c.code {
				t.Fatalf("got %d, want %d", w.Code, c.code)
			}
		})
	}
}

func TestPreviewImage_PosterUrlCarriesBadgeFingerprint(t *testing.T) {
	h := posterEnv(t, config.CoverBadges{Quality: true})
	vd, err := h.libraryService.GetScene(context.Background(), "1", false)
	if err != nil {
		t.Fatal(err)
	}

	got := previewImage(vd, "https://vr.example", coverbadge.CurrentURLQuery())

	cfg := config.Application()
	want := "https://vr.example/api/playa/v2/poster/1" + coverbadge.URLQuery(cfg.CoverBadges, cfg.VideoRules, cfg.HeatmapHeightPx)
	if got == nil || *got != want || want == "https://vr.example/api/playa/v2/poster/1" {
		t.Fatalf("expected %q, got %v", want, got)
	}
}
