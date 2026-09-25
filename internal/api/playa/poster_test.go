package playa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Khan/genqlient/graphql"

	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

var posterSky = color.RGBA{R: 40, G: 90, B: 160, A: 255}

// posterStash answers FindScenes with one 8K scene whose screenshot is
// served by base.
type posterStash struct{ base string }

func (s *posterStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	payload := `{}`
	if req.OpName == "FindScenes" {
		payload = fmt.Sprintf(`{"findScenes":{"scenes":[{"id":"1","title":"One","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":60,"path":"/s.mp4","width":8192,"height":4096,"video_codec":"hevc"}],"tags":[{"id":"8","name":"8K","sort_name":"","aliases":[],"parents":[]}],"interactive":false,"paths":{"screenshot":"%s/shot","stream":"%s/stream"}}]}}`, s.base, s.base)
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func posterEnv(t *testing.T, badges config.CoverBadges) httpHandler {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.SetRGBA(x, y, posterSky)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(buf.Bytes())
	}))
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
	img, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if goldCorner(img) {
		t.Fatal("badges switched off must leave the poster plain")
	}
}

func TestPreviewImage_PosterUrlCarriesBadgeFingerprint(t *testing.T) {
	h := posterEnv(t, config.CoverBadges{Quality: true})
	vd, err := h.libraryService.GetScene(context.Background(), "1", false)
	if err != nil {
		t.Fatal(err)
	}

	got := previewImage(vd, "https://vr.example")

	want := "https://vr.example/api/playa/v2/poster/1" + coverbadge.URLQuery(config.Application().CoverBadges, config.Application().VideoRules)
	if got == nil || *got != want || want == "https://vr.example/api/playa/v2/poster/1" {
		t.Fatalf("expected %q, got %v", want, got)
	}
}
