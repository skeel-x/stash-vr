package playa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"time"

	_ "golang.org/x/image/webp"
)

var errPosterNotFound = errors.New("poster not found")

// posterClient bounds the plain (non-heatmap) poster fetch from Stash.
var posterClient = &http.Client{Timeout: 15 * time.Second}

// buildPoster returns the scene's poster as JPEG. Interactive scenes get
// the heatmap and scenes with cover badges get the badges, rendered and
// cached the same way as /cover; other screenshots are re-encoded as
// they are.
func buildPoster(ctx context.Context, vd *library.VideoData) ([]byte, error) {
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Paths == nil {
		return nil, errPosterNotFound
	}
	paths := vd.SceneParts.Paths
	if paths.Screenshot == nil || *paths.Screenshot == "" {
		return nil, errPosterNotFound
	}
	cfg := config.Application()
	badges := coverbadge.ForScene(vd, cfg.CoverBadges, cfg.VideoRules)
	heatmapURL := ""
	if vd.SceneParts.Interactive && paths.Interactive_heatmap != nil && *paths.Interactive_heatmap != "" {
		heatmapURL = stash.ApiKeyed(*paths.Interactive_heatmap)
	}
	if heatmapURL != "" || len(badges) > 0 {
		b, err := heatmap.RenderCover(ctx, vd.Id(), stash.ApiKeyed(*paths.Screenshot), heatmapURL, badges)
		return b, mapPosterError(err)
	}
	poster, err := fetchPosterImage(ctx, stash.ApiKeyed(*paths.Screenshot))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, poster, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fetchPosterImage(ctx context.Context, fileURL string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := posterClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", stash.Redacted(fileURL), heatmap.UnwrapURLError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errPosterNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(resp.Status)
	}

	poster, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}
	return poster, nil
}

func mapPosterError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errPosterNotFound) {
		return err
	}
	if errors.Is(err, heatmap.ErrImageNotFound()) {
		return errPosterNotFound
	}
	return err
}
