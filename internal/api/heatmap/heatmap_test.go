package heatmap

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
)

// serveFixture returns a test server that serves the named testdata file on
// /cover and either the heatmap fixture or 404 on /heatmap.
func serveFixture(t *testing.T, coverFile string, withHeatmap bool, coverDelay time.Duration) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cover", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(coverDelay)
		http.ServeFile(w, r, filepath.Join("testdata", coverFile))
	})
	mux.HandleFunc("/heatmap", func(w http.ResponseWriter, r *http.Request) {
		if !withHeatmap {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join("testdata", "heatmap.png"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func assertNotBlack(t *testing.T, img interface {
	At(x, y int) color.Color
}) {
	t.Helper()
	r, g, b, _ := img.At(2, 2).RGBA()
	if r == 0 && g == 0 && b == 0 {
		t.Fatalf("expected a non-black cover pixel, got black")
	}
}

func renderFixture(t *testing.T, srv *httptest.Server) (image.Image, error) {
	t.Helper()
	coverbadge.ResetCache()
	t.Cleanup(coverbadge.ResetCache)
	b, err := RenderCover(context.Background(), "1", srv.URL+"/cover", srv.URL+"/heatmap", nil)
	if err != nil {
		return nil, err
	}
	return jpeg.Decode(bytes.NewReader(b))
}

func TestRenderCover_DecodesWebpScreenshot(t *testing.T) {
	srv := serveFixture(t, "cover.webp", true, 0)

	cover, err := renderFixture(t, srv)
	if err != nil {
		t.Fatalf("expected webp screenshot to decode, got error: %v", err)
	}
	assertNotBlack(t, cover)
}

func TestRenderCover_DecodesGifScreenshot(t *testing.T) {
	srv := serveFixture(t, "cover.gif", true, 0)

	cover, err := renderFixture(t, srv)
	if err != nil {
		t.Fatalf("expected gif screenshot to decode, got error: %v", err)
	}
	assertNotBlack(t, cover)
}

func TestRenderCover_MissingHeatmapServesPlainScreenshot(t *testing.T) {
	srv := serveFixture(t, "cover.gif", false, 0)

	cover, err := renderFixture(t, srv)
	if err != nil {
		t.Fatalf("expected the screenshot without a heatmap, got error: %v", err)
	}
	assertNotBlack(t, cover)
}

func TestRenderCover_UndecodableHeatmapServesPlainScreenshot(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cover", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("testdata", "cover.gif"))
	})
	mux.HandleFunc("/heatmap", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not an image"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cover, err := renderFixture(t, srv)
	if err != nil {
		t.Fatalf("expected the screenshot without a heatmap, got error: %v", err)
	}
	assertNotBlack(t, cover)
}

func TestRenderCover_ReturnsErrorWhenScreenshotUndecodableAndHeatmapMissing(t *testing.T) {
	// The heatmap request fails immediately (404) while the screenshot request
	// fails later with undecodable content. The function must report an error
	// rather than returning an empty cover.
	if err := os.WriteFile(filepath.Join("testdata", "garbage.html"), []byte("<html>login</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join("testdata", "garbage.html")) })
	srv := serveFixture(t, "garbage.html", false, 100*time.Millisecond)

	if _, err := renderFixture(t, srv); err == nil {
		t.Fatal("expected an error, got nil")
	}
	if coverbadge.Rendered.Len() != 0 {
		t.Fatal("a failed render must not be cached")
	}
}

// skyPNG is a w x h sky screenshot as PNG, so pixels survive exactly.
func skyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, sky)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestComposeCover_BadgesSitAboveTheHeatmapStrip(t *testing.T) {
	setBadges(t, config.CoverBadges{})
	shot := skyPNG(t, 400, 200)
	heat, err := os.ReadFile(filepath.Join("testdata", "heatmap.png"))
	if err != nil {
		t.Fatal(err)
	}
	gold := []coverbadge.Badge{{Kind: coverbadge.KindQuality, Label: "8K", Fill: coverbadge.Gold, Text: coverbadge.Dark}}

	plain, err := composeCover(context.Background(), shot, heat, nil)
	if err != nil {
		t.Fatal(err)
	}
	badged, err := composeCover(context.Background(), shot, heat, gold)
	if err != nil {
		t.Fatal(err)
	}

	// The heatmap fixture is 4 px high: the strip covers rows 196 to 199
	// and the 18 px badge sits the 8 px inset above it, rows 170 to 187.
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			inBadge := y >= 170 && y < 188 && x >= 8 && x < 100
			if !inBadge && plain.At(x, y) != badged.At(x, y) {
				t.Fatalf("pixel %d,%d outside the badge changed", x, y)
			}
		}
	}
	if !hasColour(badged, image.Rect(8, 170, 100, 188), coverbadge.Gold) {
		t.Fatal("expected the gold badge right above the strip")
	}
}

func TestComposeCover_BadgesSitAboveTheBottomEdgeWithoutHeatmap(t *testing.T) {
	gold := []coverbadge.Badge{{Kind: coverbadge.KindQuality, Label: "8K", Fill: coverbadge.Gold, Text: coverbadge.Dark}}

	img, err := composeCover(context.Background(), skyPNG(t, 400, 200), nil, gold)
	if err != nil {
		t.Fatal(err)
	}

	if !hasColour(img, image.Rect(8, 174, 100, 192), coverbadge.Gold) {
		t.Fatal("expected the gold badge the inset above the bottom edge")
	}
	if hasColour(img, image.Rect(0, 192, 400, 200), coverbadge.Gold) {
		t.Fatal("the inset below the badge must stay clear")
	}
}
