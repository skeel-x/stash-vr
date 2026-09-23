package heatmap

import (
	"bytes"
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
	"net/url"
	"stash-vr/internal/config"
	"stash-vr/internal/stash"
	"strings"
	"time"
)

// httpClient bounds every screenshot and heatmap fetch from Stash.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// maxCoverBytes caps how much of a screenshot loadScreenshot buffers into
// memory. A variable, not a constant, so tests can lower it.
var maxCoverBytes int64 = 32 << 20

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

// fetchScreenshot fetches fileUrl from Stash, mapping a 404 to
// errImageNotFound and any other non-200 to an error. On success the caller
// owns resp.Body and must close it; on error the body is already closed.
func fetchScreenshot(ctx context.Context, fileUrl string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileUrl, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", stash.Redacted(fileUrl), UnwrapURLError(err))
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, errImageNotFound
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("stash returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// UnwrapURLError strips the URL from a *url.Error so callers can log it
// safely.
func UnwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func fetchImage(ctx context.Context, fileUrl string) (image.Image, error) {
	log.Ctx(ctx).Trace().Str("url", stash.Redacted(fileUrl)).Msg("Fetching image")
	resp, err := fetchScreenshot(ctx, fileUrl)
	if err != nil {
		if errors.Is(err, errImageNotFound) {
			log.Ctx(ctx).Debug().Msg("Image not found")
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Ctx(ctx).Trace().Str("format", format).Msg("Fetched image")
	return img, nil
}

// loadScreenshot fetches the Stash screenshot fully into memory and returns
// its content type and body. JPEG and PNG pass through unchanged; anything
// else (WebP, GIF) is transcoded to JPEG because the players cannot display
// it. Buffering the whole response lets the caller write the status,
// headers and body atomically, so a fetch or transcode failure never leaves
// a partially written, cacheable response on the wire.
func loadScreenshot(ctx context.Context, fileUrl string) (contentType string, body []byte, err error) {
	resp, err := fetchScreenshot(ctx, fileUrl)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/jpeg") || strings.HasPrefix(ct, "image/png") {
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
		if err != nil {
			return "", nil, err
		}
		if int64(len(b)) > maxCoverBytes {
			return "", nil, fmt.Errorf("screenshot larger than %d bytes", maxCoverBytes)
		}
		return ct, b, nil
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("decode screenshot: %w", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return "", nil, err
	}
	return "image/jpeg", buf.Bytes(), nil
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
