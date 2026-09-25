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
	"stash-vr/internal/api/coverbadge"
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

// coverJPEGQuality is the JPEG quality of rendered covers.
const coverJPEGQuality = 85

// ErrImageNotFound returns the sentinel error other packages use to map a
// missing screenshot to HTTP 404.
func ErrImageNotFound() error {
	return errImageNotFound
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

// fetchBytes fetches fileUrl fully into memory, at most maxCoverBytes, and
// returns its content type and body.
func fetchBytes(ctx context.Context, fileUrl string) (contentType string, body []byte, err error) {
	resp, err := fetchScreenshot(ctx, fileUrl)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		return "", nil, err
	}
	if int64(len(b)) > maxCoverBytes {
		return "", nil, fmt.Errorf("image larger than %d bytes", maxCoverBytes)
	}
	return resp.Header.Get("Content-Type"), b, nil
}

// loadScreenshot fetches the Stash screenshot fully into memory and returns
// its content type and body. JPEG and PNG pass through unchanged; anything
// else (WebP, GIF) is transcoded to JPEG because the players cannot display
// it. Buffering the whole response lets the caller write the status,
// headers and body atomically, so a fetch or transcode failure never leaves
// a partially written, cacheable response on the wire.
func loadScreenshot(ctx context.Context, fileUrl string) (contentType string, body []byte, err error) {
	ct, b, err := fetchBytes(ctx, fileUrl)
	if err != nil {
		return "", nil, err
	}
	if strings.HasPrefix(ct, "image/jpeg") || strings.HasPrefix(ct, "image/png") {
		return ct, b, nil
	}

	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", nil, fmt.Errorf("decode screenshot: %w", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return "", nil, err
	}
	return "image/jpeg", buf.Bytes(), nil
}

// RenderCover returns a scene's cover as JPEG: the screenshot at coverUrl
// with the heatmap at heatmapUrl across the bottom (when heatmapUrl is
// set and the heatmap loads) and badges drawn in the bottom left corner,
// above the heatmap strip. Covers already rendered from the same
// screenshot, heatmap and badges come from coverbadge.Rendered. A missing
// screenshot is ErrImageNotFound.
func RenderCover(ctx context.Context, sceneId string, coverUrl string, heatmapUrl string, badges []coverbadge.Badge) ([]byte, error) {
	shot, heat, err := fetchSources(ctx, coverUrl, heatmapUrl)
	if err != nil {
		return nil, err
	}

	key := coverbadge.CacheKey(sceneId, badges, shot, heat)
	if b, ok := coverbadge.Rendered.Get(key); ok {
		log.Ctx(ctx).Trace().Msg("Rendered cover from cache")
		return b, nil
	}

	b, err := renderJPEG(ctx, shot, heat, badges)
	if err != nil {
		return nil, err
	}
	coverbadge.Rendered.Add(key, b)
	return b, nil
}

// RenderPreview renders a cover the way RenderCover does, but always
// afresh and without adding it to coverbadge.Rendered: the Setup page
// previews badge settings that are not saved yet.
func RenderPreview(ctx context.Context, coverUrl string, heatmapUrl string, badges []coverbadge.Badge) ([]byte, error) {
	shot, heat, err := fetchSources(ctx, coverUrl, heatmapUrl)
	if err != nil {
		return nil, err
	}
	return renderJPEG(ctx, shot, heat, badges)
}

// fetchSources fetches the screenshot and, when heatmapUrl is set, the
// heatmap in parallel. A heatmap that does not load is left out (heat is
// nil); a screenshot that does not load is an error.
func fetchSources(ctx context.Context, coverUrl string, heatmapUrl string) (shot, heat []byte, err error) {
	var shotErr error
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		_, shot, shotErr = fetchBytes(gctx, coverUrl)
		return shotErr
	})
	if heatmapUrl != "" {
		g.Go(func() error {
			_, b, err := fetchBytes(gctx, heatmapUrl)
			if err != nil {
				// No heatmap: the plain screenshot is served instead.
				log.Ctx(ctx).Debug().Err(err).Msg("Heatmap unavailable")
				return nil
			}
			heat = b
			return nil
		})
	}
	_ = g.Wait()
	if shotErr != nil {
		return nil, nil, fmt.Errorf("screenshot: %w", shotErr)
	}
	return shot, heat, nil
}

// renderJPEG composes the cover and encodes it as JPEG.
func renderJPEG(ctx context.Context, shot, heat []byte, badges []coverbadge.Badge) ([]byte, error) {
	cover, err := composeCover(ctx, shot, heat, badges)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, cover, &jpeg.Options{Quality: coverJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode cover: %w", err)
	}
	return buf.Bytes(), nil
}

// composeCover decodes the screenshot, overlays the heatmap when heat is
// set and decodes, and draws the badges above the heatmap strip.
func composeCover(ctx context.Context, shot, heat []byte, badges []coverbadge.Badge) (image.Image, error) {
	img, format, err := image.Decode(bytes.NewReader(shot))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	log.Ctx(ctx).Trace().Str("format", format).Msg("Decoded screenshot")
	strip := 0
	if heat != nil {
		if heatmap, _, err := image.Decode(bytes.NewReader(heat)); err != nil {
			log.Ctx(ctx).Debug().Err(err).Msg("Undecodable heatmap, leaving it out")
		} else {
			dest, ok := img.(draw.Image)
			if !ok {
				dest = image.NewRGBA(img.Bounds())
				draw.Copy(dest, img.Bounds().Min, img, img.Bounds(), draw.Src, nil)
			}
			img, strip = overlay(dest, heatmap)
		}
	}
	return coverbadge.Draw(img, badges, strip), nil
}

// overlay scales heatmap across the bottom of dest and returns dest with
// the height of the strip it covers.
func overlay(dest draw.Image, heatmap image.Image) (image.Image, int) {
	destSize := dest.Bounds().Size()
	heatmapHeight := config.Application().HeatmapHeightPx
	if heatmapHeight == 0 {
		heatmapHeight = heatmap.Bounds().Size().Y
	}
	heatmapHeight = int(math.Min(float64(destSize.Y), float64(heatmapHeight)))
	draw.NearestNeighbor.Scale(dest, image.Rect(0, destSize.Y, destSize.X, destSize.Y-heatmapHeight), heatmap, heatmap.Bounds(), draw.Src, nil)
	return dest, heatmapHeight
}
