package heatmap

import (
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestBuildHeatmapCover_DecodesWebpScreenshot(t *testing.T) {
	srv := serveFixture(t, "cover.webp", true, 0)

	cover, err := buildHeatmapCover(context.Background(), srv.URL+"/cover", srv.URL+"/heatmap")
	if err != nil {
		t.Fatalf("expected webp screenshot to decode, got error: %v", err)
	}
	if cover == nil {
		t.Fatal("expected a cover image, got nil")
	}
	assertNotBlack(t, cover)
}

func TestBuildHeatmapCover_DecodesGifScreenshot(t *testing.T) {
	srv := serveFixture(t, "cover.gif", true, 0)

	cover, err := buildHeatmapCover(context.Background(), srv.URL+"/cover", srv.URL+"/heatmap")
	if err != nil {
		t.Fatalf("expected gif screenshot to decode, got error: %v", err)
	}
	if cover == nil {
		t.Fatal("expected a cover image, got nil")
	}
	assertNotBlack(t, cover)
}

func TestBuildHeatmapCover_ReturnsErrorWhenScreenshotUndecodableAndHeatmapMissing(t *testing.T) {
	// The heatmap request fails immediately (404) while the screenshot request
	// fails later with undecodable content. The function must report an error
	// rather than returning a nil image with a nil error.
	if err := os.WriteFile(filepath.Join("testdata", "garbage.html"), []byte("<html>login</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join("testdata", "garbage.html")) })
	srv := serveFixture(t, "garbage.html", false, 100*time.Millisecond)

	cover, err := buildHeatmapCover(context.Background(), srv.URL+"/cover", srv.URL+"/heatmap")
	if err == nil {
		t.Fatalf("expected an error, got nil error and cover=%v", cover)
	}
	if cover != nil {
		t.Fatalf("expected nil cover on error, got %v", cover)
	}
}
