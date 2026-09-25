package heatmap

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stash-vr/internal/api/coverbadge"
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
