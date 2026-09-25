// Package hsp serves HereSphere profiles: the one stored for a scene, else
// the one learned from its studio and lens, else the one captured for its
// video rule, else one generated from the rules' screen settings.
package hsp

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// Generator encodes the profile generated for a scene from its resolved
// format. It is injected so this package does not depend on the HereSphere
// API package, which owns the tag list the profile repeats.
type Generator func(r *http.Request, vd *library.VideoData, f library.Format) ([]byte, error)

func Handler(libraryService *library.Service, generate Generator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "videoId")
		if libraryService.HasProfile(id) {
			serveStored(w, r, libraryService.ProfilePath(id))
			return
		}
		if !isSceneId(id) {
			http.NotFound(w, r)
			return
		}
		ctx := r.Context()
		vd, err := libraryService.GetScene(ctx, id, false)
		if err != nil {
			if errors.Is(err, library.ErrSceneNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Ctx(ctx).Warn().Err(err).Str("scene", id).Msg("Failed to get scene for HereSphere profile")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		f := library.ResolveFormat(config.Application().VideoRules, vd.SceneParts.Tags)
		source, scene := library.ProfileSourceFor(id, f, libraryService.HasProfile, func() string {
			return libraryService.StudioProfile(ctx, vd, &f)
		})
		switch {
		case source == library.ProfileOwn || source == library.ProfileStudio || source == library.ProfileRule:
			serveStored(w, r, libraryService.ProfilePath(scene))
		case source == library.ProfileGenerated && generate != nil:
			data, err := generate(r, vd, f)
			if err != nil {
				log.Ctx(ctx).Warn().Err(err).Str("scene", id).Msg("Failed to generate HereSphere profile")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			setHeaders(w)
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}
}

func serveStored(w http.ResponseWriter, r *http.Request, path string) {
	setHeaders(w)
	http.ServeFile(w, r, path)
}

func setHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
}

func isSceneId(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
