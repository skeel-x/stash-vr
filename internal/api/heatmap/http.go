package heatmap

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"image/jpeg"
	"net/http"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
)

func CoverHandler(libraryService *library.Service) http.HandlerFunc {
	f := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sceneId := chi.URLParam(r, "videoId")

		vd, err := libraryService.GetScene(ctx, sceneId, false)
		if err != nil {
			log.Ctx(ctx).Debug().Msg("Scene not found")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNotFound)
			return
		}

		p := vd.SceneParts.Paths
		if p == nil || p.Screenshot == nil || *p.Screenshot == "" {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if vd.SceneParts.Interactive && p.Interactive_heatmap != nil && *p.Interactive_heatmap != "" {
			cover, err := buildHeatmapCover(ctx, stash.ApiKeyed(*p.Screenshot), stash.ApiKeyed(*p.Interactive_heatmap))
			if err != nil {
				log.Ctx(ctx).Err(err).Msg("buildHeatmapCover")
				w.Header().Set("Cache-Control", "no-store")
				if errors.Is(err, errImageNotFound) {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.WriteHeader(http.StatusBadGateway)
				}
				return
			}
			w.Header().Set("Cache-Control", "private, max-age=86400")
			w.Header().Set("Content-Type", "image/jpeg")
			if err := jpeg.Encode(w, cover, nil); err != nil {
				log.Ctx(ctx).Err(err).Msg("cover: write")
			}
			return
		}

		w.Header().Set("Cache-Control", "private, max-age=86400")
		if err := serveScreenshot(ctx, w, stash.ApiKeyed(*p.Screenshot)); err != nil {
			log.Ctx(ctx).Err(err).Msg("serveScreenshot")
			w.Header().Set("Cache-Control", "no-store")
			if errors.Is(err, errImageNotFound) {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusBadGateway)
			}
		}
	}
	return internal.LogRoute("cover", internal.LogVideoId(f))
}

func GetCoverUrl(baseUrl string, sceneId string) string {
	return baseUrl + "/cover/" + sceneId
}
