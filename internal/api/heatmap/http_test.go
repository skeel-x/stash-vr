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
	"stash-vr/internal/api/coverbadge"
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
	big := image.NewRGBA(image.Rect(0, 0, bigW, bigH))
	for y := 0; y < bigH; y++ {
		for x := 0; x < bigW; x++ {
			big.Set(x, y, sky)
		}
	}
	var bigJpg bytes.Buffer
	if err := jpeg.Encode(&bigJpg, big, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	bigCover = bigJpg.Bytes()
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(bigCover)
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
	mux.HandleFunc("/huge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		chunk := make([]byte, 64<<10)
		remaining := maxCoverBytes + 1
		for remaining > 0 {
			n := int64(len(chunk))
			if remaining < n {
				n = remaining
			}
			if _, err := w.Write(chunk[:n]); err != nil {
				return
			}
			remaining -= n
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, jpg.Bytes(), pn.Bytes()
}

// bigW and bigH size the /big fixture, large enough to carry badges.
const bigW, bigH = 400, 200

var sky = color.RGBA{R: 40, G: 90, B: 160, A: 255}

// bigCover is the /big fixture's bytes, set by imageServer.
var bigCover []byte

// badgeScenes are the scenes with tags and a file: id, screenshot path,
// interactive, tag names.
var badgeScenes = map[int]struct {
	shot        string
	interactive bool
	tags        []string
}{
	10: {"/big", false, []string{"8K", "HQ"}},
	11: {"/big", false, []string{"DOME"}},
	12: {"/big", true, []string{"7K"}},
	13: {"/missing", false, []string{"8K"}},
}

// sceneStash answers FindScenes with one scene per requested id, whose
// screenshot path is chosen by id. Ids in failIDs make MakeRequest return an
// error instead, simulating Stash being unreachable.
type sceneStash struct {
	base    string
	failIDs map[int]bool
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
	for _, id := range in.SceneIDs {
		if s.failIDs[id] {
			return fmt.Errorf("stash unreachable")
		}
	}
	var scenes []string
	for _, id := range in.SceneIDs {
		if b, ok := badgeScenes[id]; ok {
			var tags []string
			for _, n := range b.tags {
				tags = append(tags, fmt.Sprintf(`{"id":"%s","name":"%s","sort_name":"","aliases":[],"parents":[]}`, n, n))
			}
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"S%d","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60,"path":"/s.mp4","width":8192,"height":4096,"video_codec":"hevc"}],"tags":[%s],"interactive":%t,"paths":{"screenshot":"%s%s","interactive_heatmap":"%s/heatmap","stream":"%s/stream","preview":"","funscript":"","caption":""}}`, id, id, strings.Join(tags, ","), b.interactive, s.base, b.shot, s.base, s.base))
			continue
		}
		shot := map[int]string{1: "/jpeg", 2: "/webp", 3: "/png", 4: "/missing", 5: "/jpeg", 6: "/broken", 9: "/huge"}[id]
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

// setMaxCoverBytesForTest lowers the package's cover size cap for the
// duration of the calling test, so oversized-fixture tests do not need to
// allocate and stream tens of megabytes.
func setMaxCoverBytesForTest(t *testing.T, n int64) {
	t.Helper()
	prev := maxCoverBytes
	maxCoverBytes = n
	t.Cleanup(func() { maxCoverBytes = prev })
}

func TestCover_OversizedScreenshotIs502(t *testing.T) {
	setMaxCoverBytesForTest(t, 1<<16)
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/9")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store for an oversized screenshot, got %q", got)
	}
}

func TestCover_StashDownIs502(t *testing.T) {
	srv, _, _ := imageServer(t)
	stash := &sceneStash{base: srv.URL, failIDs: map[int]bool{8: true}}
	lib := library.NewService(stash)
	r := chi.NewRouter()
	r.Get("/cover/{videoId}", CoverHandler(lib))

	rec := get(t, r, "/cover/8")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store for a Stash outage, got %q", got)
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

// setBadges loads settings with the given cover badges and the default
// video rules, and empties the rendered cover cache around the test.
func setBadges(t *testing.T, b config.CoverBadges) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress:    ":9666",
		StashGraphQLUrl:  "http://stash:9999/graphql",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		ConfigPath:       t.TempDir(),
		CoverBadges:      b,
	}); err != nil {
		t.Fatal(err)
	}
	coverbadge.ResetCache()
	t.Cleanup(func() {
		coverbadge.ResetCache()
		if err := config.Load(config.ApplicationConfig{
			ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", LogLevel: "info",
			ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func decodeJpeg(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("body is not a jpeg: %v", err)
	}
	return img
}

func near(c color.Color, want color.RGBA, tol int) bool {
	r, g, b, _ := c.RGBA()
	d := func(x uint32, y uint8) int {
		v := int(x>>8) - int(y)
		if v < 0 {
			return -v
		}
		return v
	}
	return d(r, want.R) <= tol && d(g, want.G) <= tol && d(b, want.B) <= tol
}

// hasColour reports whether any pixel in r is near want.
func hasColour(img image.Image, r image.Rectangle, want color.RGBA) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if near(img.At(x, y), want, 12) {
				return true
			}
		}
	}
	return false
}

func TestCover_QualityBadgeIsDrawn(t *testing.T) {
	setBadges(t, config.CoverBadges{Quality: true})
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/10")

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatalf("expected the day-long cache header, got %q", rec.Header().Get("Cache-Control"))
	}
	img := decodeJpeg(t, rec.Body.Bytes())
	if img.Bounds() != image.Rect(0, 0, bigW, bigH) {
		t.Fatalf("size changed: %v", img.Bounds())
	}
	if !hasColour(img, image.Rect(0, 0, bigW/4, bigH/4), coverbadge.Gold) {
		t.Fatal("expected a gold 8K badge in the top left corner")
	}
	if !near(img.At(bigW/2, bigH*3/4), sky, 12) {
		t.Fatal("the rest of the cover must stay as it was")
	}
}

func TestCover_NoApplicableBadgePassesThrough(t *testing.T) {
	for name, b := range map[string]config.CoverBadges{
		"all off":             {},
		"format without rule": {Format: true},
	} {
		t.Run(name, func(t *testing.T) {
			setBadges(t, b)
			srv, _, _ := imageServer(t)
			h := coverRouter(t, srv.URL)

			rec := get(t, h, "/cover/10")

			if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), bigCover) {
				t.Fatalf("expected the screenshot byte-identical, got %d with %d bytes", rec.Code, rec.Body.Len())
			}
			if coverbadge.Rendered.Len() != 0 {
				t.Fatal("a pass-through cover must not be cached")
			}
		})
	}
}

func TestCover_FormatBadge(t *testing.T) {
	setBadges(t, config.CoverBadges{Format: true})
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/11")

	if rec.Code != 200 || bytes.Equal(rec.Body.Bytes(), bigCover) {
		t.Fatalf("expected a re-encoded cover, got %d", rec.Code)
	}
	if !hasColour(decodeJpeg(t, rec.Body.Bytes()), image.Rect(0, 0, bigW/4, bigH/4), coverbadge.Neutral) {
		t.Fatal("expected the 180 badge in the top left corner")
	}
}

func TestCover_RenderedCoverIsCached(t *testing.T) {
	setBadges(t, config.CoverBadges{Quality: true})
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	first := get(t, h, "/cover/10").Body.Bytes()
	if coverbadge.Rendered.Len() != 1 {
		t.Fatalf("expected one rendered cover cached, got %d", coverbadge.Rendered.Len())
	}
	second := get(t, h, "/cover/10").Body.Bytes()

	if !bytes.Equal(first, second) || coverbadge.Rendered.Len() != 1 {
		t.Fatal("expected the second request served from the cache")
	}
	key := coverbadge.CacheKey("10", []coverbadge.Badge{{Kind: coverbadge.KindQuality, Label: "8K"}}, bigCover, nil)
	if _, ok := coverbadge.Rendered.Get(key); !ok {
		t.Fatal("expected the cover cached under scene, badges and screenshot digest")
	}
}

func TestCover_BadgesOnHeatmapCover(t *testing.T) {
	setBadges(t, config.CoverBadges{Quality: true})
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/12")

	if rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
	img := decodeJpeg(t, rec.Body.Bytes())
	if !hasColour(img, image.Rect(0, 0, bigW/4, bigH/4), coverbadge.Silver) {
		t.Fatal("expected a silver 7K badge")
	}
	if near(img.At(bigW/2, bigH-1), sky, 12) {
		t.Fatal("expected the heatmap across the bottom")
	}
}

func TestCover_BadgedSceneWithMissingScreenshotIs404(t *testing.T) {
	setBadges(t, config.CoverBadges{Quality: true})
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	rec := get(t, h, "/cover/13")

	if rec.Code != 404 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected an uncached 404, got %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestGetCoverUrl_CarriesBadgeFingerprint(t *testing.T) {
	setBadges(t, config.CoverBadges{})
	if got := GetCoverUrl("https://vr.example", "9"); got != "https://vr.example/cover/9" {
		t.Fatalf("badges off must keep the plain url, got %q", got)
	}

	cfg := config.Application()
	cfg.CoverBadges = config.CoverBadges{Quality: true}
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	quality := GetCoverUrl("https://vr.example", "9")
	if !strings.HasPrefix(quality, "https://vr.example/cover/9?b=") {
		t.Fatalf("expected a badge fingerprint, got %q", quality)
	}

	cfg.CoverBadges.Format = true
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := GetCoverUrl("https://vr.example", "9"); got == quality {
		t.Fatal("toggling a badge must change the cover url")
	}
}

func TestCover_IgnoresFingerprintQuery(t *testing.T) {
	setBadges(t, config.CoverBadges{Quality: true})
	srv, _, _ := imageServer(t)
	h := coverRouter(t, srv.URL)

	if rec := get(t, h, "/cover/10?b=abcd1234"); rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
