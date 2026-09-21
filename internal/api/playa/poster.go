package playa

import (
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"

	_ "golang.org/x/image/webp"
)

var errPosterNotFound = errors.New("poster not found")

func buildPosterImage(ctx context.Context, vd *library.VideoData) (image.Image, error) {
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Paths == nil {
		return nil, errPosterNotFound
	}
	paths := vd.SceneParts.Paths
	if paths.Screenshot == nil || *paths.Screenshot == "" {
		return nil, errPosterNotFound
	}
	if vd.SceneParts.Interactive && paths.Interactive_heatmap != nil && *paths.Interactive_heatmap != "" {
		return buildHeatmapPoster(ctx, *paths.Screenshot, *paths.Interactive_heatmap)
	}
	return fetchPosterImage(ctx, stash.ApiKeyed(*paths.Screenshot))
}

func buildHeatmapPoster(ctx context.Context, screenshotURL string, heatmapURL string) (image.Image, error) {
	coverURL := stash.ApiKeyed(screenshotURL)
	mapURL := stash.ApiKeyed(heatmapURL)
	poster, err := heatmapBuildCover(ctx, coverURL, mapURL)
	if err != nil {
		return nil, mapPosterError(err)
	}
	return poster, nil
}

func fetchPosterImage(ctx context.Context, fileURL string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

func heatmapBuildCover(ctx context.Context, coverURL string, heatmapURL string) (image.Image, error) {
	return heatmap.BuildCover(ctx, coverURL, heatmapURL)
}
