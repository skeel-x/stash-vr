package playa

import (
	"context"
	"errors"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
)

var errPosterNotFound = errors.New("poster not found")

// poster is a scene's poster ready to send. degraded is set when the
// heatmap could not be fetched for a reason that may pass and the poster
// was rendered without it: it should then not be cached by the player.
type poster struct {
	contentType string
	body        []byte
	degraded    error
}

// buildPoster returns the scene's poster. Interactive scenes get the
// heatmap and scenes with cover badges get the badges, rendered and cached
// the same way as /cover; other screenshots are served as Stash holds them
// (JPEG and PNG as they are, anything else transcoded to JPEG once) through
// the same loader as /cover, so the byte cap and the TLS setting apply
// here too.
func buildPoster(ctx context.Context, vd *library.VideoData) (poster, error) {
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Paths == nil {
		return poster{}, errPosterNotFound
	}
	paths := vd.SceneParts.Paths
	if paths.Screenshot == nil || *paths.Screenshot == "" {
		return poster{}, errPosterNotFound
	}
	cfg := config.Application()
	badges := coverbadge.ForScene(vd, cfg.CoverBadges, cfg.VideoRules)
	heatmapURL := heatmap.SceneHeatmapURL(vd)
	if heatmapURL != "" || len(badges) > 0 {
		b, degraded, err := heatmap.RenderCover(ctx, vd.Id(), stash.ApiKeyed(*paths.Screenshot), heatmapURL, badges)
		if err != nil {
			return poster{}, mapPosterError(err)
		}
		return poster{contentType: "image/jpeg", body: b, degraded: degraded}, nil
	}
	ct, b, err := heatmap.LoadScreenshot(ctx, stash.ApiKeyed(*paths.Screenshot))
	if err != nil {
		return poster{}, mapPosterError(err)
	}
	return poster{contentType: ct, body: b}, nil
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
