package heatmap

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/sync/errgroup"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"stash-vr/internal/config"
	"strings"
	"time"
)

// httpClient bounds every screenshot and heatmap fetch from Stash.
var httpClient = &http.Client{Timeout: 15 * time.Second}

var errImageNotFound = errors.New("image not found")
var errScreenshotImageNotFound = errors.New("screenshot image not found")
var errHeatmapImageNotFound = errors.New("heatmap image not found")

// ErrImageNotFound returns the sentinel error other packages use to map a
// missing screenshot to HTTP 404.
func ErrImageNotFound() error {
	return errImageNotFound
}

// BuildCover fetches the scene screenshot and, when available, overlays the
// interactive heatmap. It is the exported entry point used by the Playa
// poster endpoint.
func BuildCover(ctx context.Context, coverUrl string, heatmapUrl string) (image.Image, error) {
	return buildHeatmapCover(ctx, coverUrl, heatmapUrl)
}

func fetchImage(ctx context.Context, fileUrl string) (image.Image, error) {
	log.Ctx(ctx).Trace().Str("url", fileUrl).Msg("Fetching image")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileUrl, nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		log.Ctx(ctx).Debug().Msg("Image not found")
		return nil, errImageNotFound
	}

	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Ctx(ctx).Trace().Str("format", format).Msg("Fetched image")
	return img, nil
}

// serveScreenshot streams the Stash screenshot to w. JPEG and PNG pass
// through unchanged; anything else (WebP, GIF) is transcoded to JPEG because
// the players cannot display it.
func serveScreenshot(ctx context.Context, w http.ResponseWriter, fileUrl string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileUrl, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errImageNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("screenshot: stash returned HTTP %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/jpeg") || strings.HasPrefix(ct, "image/png") {
		w.Header().Set("Content-Type", ct)
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		_, err = io.Copy(w, resp.Body)
		return err
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("decode screenshot: %w", err)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	return jpeg.Encode(w, img, nil)
}

func buildHeatmapCover(ctx context.Context, coverUrl string, heatmapUrl string) (image.Image, error) {
	var (
		cover      draw.Image
		coverErr   error
		heatmap    image.Image
		heatmapErr error
	)

	g, _ := errgroup.WithContext(ctx)

	g.Go(func() error {
		img, err := fetchImage(log.Ctx(ctx).With().Str("image", "cover").Logger().WithContext(ctx), coverUrl)
		if err != nil {
			coverErr = errors.Join(errScreenshotImageNotFound, err)
			return coverErr
		}
		dest, ok := img.(draw.Image)
		if !ok {
			dest = image.NewRGBA(img.Bounds())
			draw.Copy(dest, image.Pt(0, 0), img, img.Bounds(), draw.Src, nil)
		}
		cover = dest
		return nil
	})

	g.Go(func() error {
		img, err := fetchImage(log.Ctx(ctx).With().Str("image", "heatmap").Logger().WithContext(ctx), heatmapUrl)
		if err != nil {
			heatmapErr = errors.Join(errHeatmapImageNotFound, err)
			return heatmapErr
		}
		heatmap = img
		return nil
	})

	_ = g.Wait()

	if coverErr != nil {
		return nil, coverErr
	}
	if heatmapErr != nil {
		// No heatmap available: serve the plain screenshot instead.
		return cover, nil
	}

	return overlay(cover, heatmap), nil
}

func overlay(dest draw.Image, heatmap image.Image) image.Image {
	destSize := dest.Bounds().Size()
	heatmapHeight := config.Application().HeatmapHeightPx
	if heatmapHeight == 0 {
		heatmapHeight = heatmap.Bounds().Size().Y
	}
	heatmapHeight = int(math.Min(float64(destSize.Y), float64(heatmapHeight)))
	draw.NearestNeighbor.Scale(dest, image.Rect(0, destSize.Y, destSize.X, destSize.Y-heatmapHeight), heatmap, heatmap.Bounds(), draw.Src, nil)
	return dest
}
