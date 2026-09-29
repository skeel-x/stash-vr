package heatmap

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// countingFixture serves the gif cover and png heatmap fixtures after delay
// and counts the cover fetches.
func countingFixture(t *testing.T, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var fetches atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/cover", func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		time.Sleep(delay)
		http.ServeFile(w, r, filepath.Join("testdata", "cover.gif"))
	})
	mux.HandleFunc("/heatmap", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("testdata", "heatmap.png"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &fetches
}

func TestRenderCover_ConcurrentRequestsShareOneFetchAndRender(t *testing.T) {
	coverbadge.ResetCache()
	t.Cleanup(coverbadge.ResetCache)
	srv, fetches := countingFixture(t, 150*time.Millisecond)

	const callers = 8
	results := make([][]byte, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = RenderCover(context.Background(), "1", srv.URL+"/cover", srv.URL+"/heatmap", nil)
		}(i)
	}
	wg.Wait()

	for i := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if !bytes.Equal(results[i], results[0]) {
			t.Fatal("every caller must get the same cover")
		}
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("expected one screenshot fetch for %d concurrent requests, got %d", callers, n)
	}
	if coverbadge.Rendered.Len() != 1 {
		t.Fatalf("expected one rendered cover cached, got %d", coverbadge.Rendered.Len())
	}
}

func TestRenderCover_CallerGivingUpDoesNotWaitForTheLeader(t *testing.T) {
	coverbadge.ResetCache()
	t.Cleanup(coverbadge.ResetCache)
	srv, _ := countingFixture(t, 300*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := RenderCover(ctx, "1", srv.URL+"/cover", srv.URL+"/heatmap", nil)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller's deadline, got %v", err)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("a caller that gave up must not wait for the render")
	}
	// The leader carries on detached from the caller, so the next request
	// finds the cover rendered.
	deadline := time.Now().Add(3 * time.Second)
	for coverbadge.Rendered.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if coverbadge.Rendered.Len() != 1 {
		t.Fatal("expected the leader to finish the render for the callers after it")
	}
}

func TestRenderSlots_BoundRendersAndHonourTheContext(t *testing.T) {
	if n := renderConcurrency(); n < 2 || n > 8 || cap(renderSlots) != n {
		t.Fatalf("expected between 2 and 8 render slots, got %d (cap %d)", n, cap(renderSlots))
	}
	var releases []func()
	for i := 0; i < cap(renderSlots); i++ {
		release, err := acquireRenderSlot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	t.Cleanup(func() {
		for _, r := range releases {
			r()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := acquireRenderSlot(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("with every slot taken the wait must end with the context, got %v", err)
	}
	if _, err := renderJPEG(ctx, skyPNG(t, 16, 12), nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a render must wait for a slot, got %v", err)
	}

	releases[0]()
	releases = releases[1:]
	release, err := acquireRenderSlot(context.Background())
	if err != nil {
		t.Fatalf("a released slot must be free again, got %v", err)
	}
	releases = append(releases, release)
}

func TestComposeCover_HeatmapAndBadgesLandOnOneImage(t *testing.T) {
	setBadges(t, config.CoverBadges{})
	heat, err := os.ReadFile(filepath.Join("testdata", "heatmap.png"))
	if err != nil {
		t.Fatal(err)
	}
	gold := []coverbadge.Badge{{Kind: coverbadge.KindQuality, Label: "8K", Fill: coverbadge.Gold, Text: coverbadge.Dark}}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 400, 200)), nil); err != nil {
		t.Fatal(err)
	}

	for name, shot := range map[string][]byte{"png": skyPNG(t, 400, 200), "jpeg": jpg.Bytes()} {
		t.Run(name, func(t *testing.T) {
			img, err := composeCover(context.Background(), shot, heat, gold)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 400, 200) {
				t.Fatalf("size changed: %v", img.Bounds())
			}
			if !hasColour(img, image.Rect(8, 170, 100, 188), coverbadge.Gold) {
				t.Fatal("expected the gold badge above the strip")
			}
			if near(img.At(200, 199), sky, 12) || near(img.At(200, 199), color.RGBA{A: 255}, 12) {
				t.Fatal("expected the heatmap across the bottom row")
			}
		})
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
