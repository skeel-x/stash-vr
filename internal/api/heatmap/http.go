package heatmap

import (
	"bytes"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"image/jpeg"
	"net/http"
	"stash-vr/internal/api/internal"
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
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, cover, nil); err != nil {
				log.Ctx(ctx).Err(err).Msg("cover: encode")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Header().Set("Cache-Control", "private, max-age=86400")
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
			if _, err := w.Write(buf.Bytes()); err != nil {
				log.Ctx(ctx).Err(err).Msg("cover: write")
			}
			return
		}

		ct, body, err := loadScreenshot(ctx, stash.ApiKeyed(*p.Screenshot))
		if err != nil {
			log.Ctx(ctx).Err(err).Msg("loadScreenshot")
			w.Header().Set("Cache-Control", "no-store")
			if errors.Is(err, errImageNotFound) {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusBadGateway)
			}
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if _, err := w.Write(body); err != nil {
			log.Ctx(ctx).Err(err).Msg("cover: write")
		}
	}
	return internal.LogRoute("cover", internal.LogVideoId(f))
}

func GetCoverUrl(baseUrl string, sceneId string) string {
	return baseUrl + "/cover/" + sceneId
}
