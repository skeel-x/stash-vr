package heatmap

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"net/http"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"strconv"
)

func CoverHandler(libraryService *library.Service) http.HandlerFunc {
	f := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sceneId := chi.URLParam(r, "videoId")

		vd, err := libraryService.GetScene(ctx, sceneId, false)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			if errors.Is(err, library.ErrSceneNotFound) {
				log.Ctx(ctx).Debug().Msg("Scene not found")
				w.WriteHeader(http.StatusNotFound)
			} else {
				log.Ctx(ctx).Err(err).Msg("GetScene")
				w.WriteHeader(http.StatusBadGateway)
			}
			return
		}

		p := vd.SceneParts.Paths
		if p == nil || p.Screenshot == nil || *p.Screenshot == "" {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNotFound)
			return
		}

		cfg := config.Application()
		badges := coverbadge.ForScene(vd, cfg.CoverBadges, cfg.VideoRules)
		heatmapUrl := SceneHeatmapURL(vd)
		if heatmapUrl != "" || len(badges) > 0 {
			body, degraded, err := RenderCover(ctx, vd.Id(), stash.ApiKeyed(*p.Screenshot), heatmapUrl, badges)
			if err != nil {
				log.Ctx(ctx).Err(err).Msg("RenderCover")
				writeFailure(w, err)
				return
			}
			if degraded != nil {
				// Sent uncached, so the headset asks again rather than
				// keeping a heatmap-less cover for a day.
				log.Ctx(ctx).Warn().Err(degraded).Msg("Heatmap unavailable, serving the cover without it uncached")
				writeCover(ctx, w, "image/jpeg", body, noStore)
				return
			}
			writeCover(ctx, w, "image/jpeg", body, keepForADay)
			return
		}

		ct, body, err := LoadScreenshot(ctx, stash.ApiKeyed(*p.Screenshot))
		if err != nil {
			log.Ctx(ctx).Err(err).Msg("LoadScreenshot")
			writeFailure(w, err)
			return
		}
		writeCover(ctx, w, ct, body, keepForADay)
	}
	return internal.LogRoute("cover", internal.LogVideoId(f))
}

// Cache-Control values of a cover: one the headset may keep for a day,
// one it must ask for again.
const (
	keepForADay = "private, max-age=86400"
	noStore     = "no-store"
)

// SceneHeatmapURL is the keyed address of the heatmap Stash generates for
// an interactive scene, or "" when the scene has none.
func SceneHeatmapURL(vd *library.VideoData) string {
	if vd == nil || vd.SceneParts == nil || !vd.SceneParts.Interactive {
		return ""
	}
	p := vd.SceneParts.Paths
	if p == nil || p.Interactive_heatmap == nil || *p.Interactive_heatmap == "" {
		return ""
	}
	return stash.ApiKeyed(*p.Interactive_heatmap)
}

// writeFailure answers a failed cover uncached: 404 for a missing
// screenshot, 502 for anything else.
func writeFailure(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", noStore)
	if errors.Is(err, errImageNotFound) {
		w.WriteHeader(http.StatusNotFound)
	} else {
		w.WriteHeader(http.StatusBadGateway)
	}
}

// writeCover sends a cover under the given Cache-Control.
func writeCover(ctx context.Context, w http.ResponseWriter, contentType string, body []byte, cacheControl string) {
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if _, err := w.Write(body); err != nil {
		log.Ctx(ctx).Err(err).Msg("cover: write")
	}
}

// GetCoverUrl is the cover URL handed to players. It carries the
// fingerprint of the badge settings and the heatmap height, so a settings
// change reaches headsets that keep covers for a day.
func GetCoverUrl(baseUrl string, sceneId string) string {
	return baseUrl + "/cover/" + sceneId + coverbadge.CurrentURLQuery()
}
