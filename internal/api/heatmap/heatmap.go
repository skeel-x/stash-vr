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
	"golang.org/x/sync/singleflight"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/stash"
	"strings"
	"time"
)

// httpClient bounds every screenshot and heatmap fetch from Stash. It runs
// on the transport the GraphQL client uses, so an https Stash with a
// self-signed certificate serves covers under the same TLS setting.
var httpClient = stash.HTTPClient(15 * time.Second)

// maxCoverBytes caps how much of a screenshot LoadScreenshot buffers into
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

// LoadScreenshot fetches the Stash screenshot fully into memory, at most
// maxCoverBytes, and returns its content type and body. JPEG and PNG pass
// through unchanged; anything else (WebP, GIF) is transcoded to JPEG
// because the players cannot display it. Buffering the whole response lets
// the caller write the status, headers and body atomically, so a fetch or
// transcode failure never leaves a partially written, cacheable response
// on the wire. A missing screenshot is ErrImageNotFound.
func LoadScreenshot(ctx context.Context, fileUrl string) (contentType string, body []byte, err error) {
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

// renderGroup collapses concurrent requests for the same cover into one
// fetch and render: a headset opening a page asks for many covers at once,
// and two players may ask for the same scene.
var renderGroup singleflight.Group

// renderTimeout bounds the work a renderGroup leader does on behalf of the
// callers sharing its result. The leader runs detached from the context of
// whichever request started it, so that request dropping does not fail the
// others, and under this deadline so it cannot hang them.
const renderTimeout = 60 * time.Second

// renderSlots bounds how many covers are decoded, composed and encoded at
// once: each holds a full RGBA copy of its screenshot and takes a CPU for
// a while, and a headset asks for a page of covers in one burst.
var renderSlots = make(chan struct{}, renderConcurrency())

// renderConcurrency is the number of renders allowed at once: one per
// CPU, at least 2, at most 8.
func renderConcurrency() int {
	return min(8, max(2, runtime.NumCPU()))
}

// acquireRenderSlot waits for a render slot, or for ctx to end, and
// returns the function that gives the slot back.
func acquireRenderSlot(ctx context.Context) (release func(), err error) {
	select {
	case renderSlots <- struct{}{}:
		return func() { <-renderSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// rendered is one renderCover result: the JPEG and, when the heatmap
// could not be fetched for a reason that may pass, that failure.
type rendered struct {
	body     []byte
	degraded error
}

// RenderCover returns a scene's cover as JPEG: the screenshot at coverUrl
// with the heatmap at heatmapUrl across the bottom (when heatmapUrl is
// set and the heatmap loads) and badges drawn in the bottom left corner,
// above the heatmap strip. Covers already rendered from the same
// screenshot, heatmap and badges come from coverbadge.Rendered, and
// concurrent calls for the same cover share one fetch and render. A
// missing screenshot is ErrImageNotFound.
//
// degraded is set when the heatmap could not be fetched for a reason that
// may pass (Stash answered an error or did not answer), so the cover was
// rendered without it: the caller should not let a headset keep such a
// cover. A heatmap Stash does not have (404) is not a degradation.
func RenderCover(ctx context.Context, sceneId string, coverUrl string, heatmapUrl string, badges []coverbadge.Badge) (body []byte, degraded error, err error) {
	key := sceneId + "\x00" + coverbadge.Key(badges) + "\x00" + coverUrl + "\x00" + heatmapUrl
	ch := renderGroup.DoChan(key, func() (interface{}, error) {
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), renderTimeout)
		defer cancel()
		return renderCover(lctx, sceneId, coverUrl, heatmapUrl, badges)
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return nil, nil, r.Err
		}
		if r.Shared {
			log.Ctx(ctx).Trace().Msg("Rendered cover shared with a concurrent request")
		}
		res := r.Val.(rendered)
		return res.body, res.degraded, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// renderCover fetches the sources, answers from coverbadge.Rendered when
// the same cover was rendered before, else renders and caches it.
func renderCover(ctx context.Context, sceneId string, coverUrl string, heatmapUrl string, badges []coverbadge.Badge) (rendered, error) {
	shot, heat, degraded, err := fetchSources(ctx, coverUrl, heatmapUrl)
	if err != nil {
		return rendered{}, err
	}

	key := coverbadge.CacheKey(sceneId, badges, shot, heat)
	if b, ok := coverbadge.Rendered.Get(key); ok {
		log.Ctx(ctx).Trace().Msg("Rendered cover from cache")
		return rendered{body: b, degraded: degraded}, nil
	}

	b, err := renderJPEG(ctx, shot, heat, badges)
	if err != nil {
		return rendered{}, err
	}
	coverbadge.Rendered.Add(key, b)
	return rendered{body: b, degraded: degraded}, nil
}

// RenderPreview renders a cover the way RenderCover does, but always
// afresh and without adding it to coverbadge.Rendered: the Setup page
// previews badge settings that are not saved yet.
func RenderPreview(ctx context.Context, coverUrl string, heatmapUrl string, badges []coverbadge.Badge) ([]byte, error) {
	shot, heat, _, err := fetchSources(ctx, coverUrl, heatmapUrl)
	if err != nil {
		return nil, err
	}
	return renderJPEG(ctx, shot, heat, badges)
}

// fetchSources fetches the screenshot and, when heatmapUrl is set, the
// heatmap in parallel. A heatmap that does not load is left out (heat is
// nil): when Stash has none (404) silently, otherwise (an error answer,
// no answer, too large) with the failure in degraded, since the next try
// may well get it. A screenshot that does not load is an error.
func fetchSources(ctx context.Context, coverUrl string, heatmapUrl string) (shot, heat []byte, degraded error, err error) {
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
				if !errors.Is(err, errImageNotFound) {
					degraded = fmt.Errorf("heatmap: %w", err)
				}
				return nil
			}
			heat = b
			return nil
		})
	}
	_ = g.Wait()
	if shotErr != nil {
		return nil, nil, nil, fmt.Errorf("screenshot: %w", shotErr)
	}
	return shot, heat, degraded, nil
}

// renderJPEG composes the cover and encodes it as JPEG, holding a render
// slot meanwhile.
func renderJPEG(ctx context.Context, shot, heat []byte, badges []coverbadge.Badge) ([]byte, error) {
	release, err := acquireRenderSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
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

// composeCover decodes the screenshot into one RGBA, overlays the heatmap
// on it when heat is set and decodes, and draws the badges on it above the
// heatmap strip. The decoded screenshot is copied at most once: not at all
// when it decodes to RGBA already.
func composeCover(ctx context.Context, shot, heat []byte, badges []coverbadge.Badge) (*image.RGBA, error) {
	img, format, err := image.Decode(bytes.NewReader(shot))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	log.Ctx(ctx).Trace().Str("format", format).Msg("Decoded screenshot")
	dst, ok := img.(*image.RGBA)
	if !ok {
		dst = image.NewRGBA(img.Bounds())
		draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Src)
	}
	strip := 0
	if heat != nil {
		if heatmap, _, err := image.Decode(bytes.NewReader(heat)); err != nil {
			log.Ctx(ctx).Debug().Err(err).Msg("Undecodable heatmap, leaving it out")
		} else {
			strip = overlay(dst, heatmap)
		}
	}
	coverbadge.DrawOn(dst, badges, strip)
	return dst, nil
}

// overlay scales heatmap across the bottom of dest, in place, and returns
// the height of the strip it covers.
func overlay(dest draw.Image, heatmap image.Image) int {
	destSize := dest.Bounds().Size()
	heatmapHeight := config.Application().HeatmapHeightPx
	if heatmapHeight == 0 {
		heatmapHeight = heatmap.Bounds().Size().Y
	}
	heatmapHeight = int(math.Min(float64(destSize.Y), float64(heatmapHeight)))
	draw.NearestNeighbor.Scale(dest, image.Rect(0, destSize.Y, destSize.X, destSize.Y-heatmapHeight), heatmap, heatmap.Bounds(), draw.Src, nil)
	return heatmapHeight
}
