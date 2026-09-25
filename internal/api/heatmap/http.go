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
		heatmapUrl := ""
		if vd.SceneParts.Interactive && p.Interactive_heatmap != nil && *p.Interactive_heatmap != "" {
			heatmapUrl = stash.ApiKeyed(*p.Interactive_heatmap)
		}
		if heatmapUrl != "" || len(badges) > 0 {
			body, err := RenderCover(ctx, vd.Id(), stash.ApiKeyed(*p.Screenshot), heatmapUrl, badges)
			if err != nil {
				log.Ctx(ctx).Err(err).Msg("RenderCover")
				writeFailure(w, err)
				return
			}
			writeCover(ctx, w, "image/jpeg", body)
			return
		}

		ct, body, err := loadScreenshot(ctx, stash.ApiKeyed(*p.Screenshot))
		if err != nil {
			log.Ctx(ctx).Err(err).Msg("loadScreenshot")
			writeFailure(w, err)
			return
		}
		writeCover(ctx, w, ct, body)
	}
	return internal.LogRoute("cover", internal.LogVideoId(f))
}

// writeFailure answers a failed cover uncached: 404 for a missing
// screenshot, 502 for anything else.
func writeFailure(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, errImageNotFound) {
		w.WriteHeader(http.StatusNotFound)
	} else {
		w.WriteHeader(http.StatusBadGateway)
	}
}

// writeCover sends a cover the headset may keep for a day.
func writeCover(ctx context.Context, w http.ResponseWriter, contentType string, body []byte) {
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if _, err := w.Write(body); err != nil {
		log.Ctx(ctx).Err(err).Msg("cover: write")
	}
}

// GetCoverUrl is the cover URL handed to players. With cover badges on it
// carries the badge settings fingerprint, so a settings change reaches
// headsets that keep covers for a day.
func GetCoverUrl(baseUrl string, sceneId string) string {
	return baseUrl + "/cover/" + sceneId + coverbadge.CurrentURLQuery()
}
