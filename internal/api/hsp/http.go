// Package hsp serves HereSphere profiles: the one stored for a scene, else
// the one learned from its studio and lens, else the one captured for its
// video rule, else one generated from the rules' screen settings and the
// scene's measured vertical stereo offset.
package hsp

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"stash-vr/internal/config"
	"stash-vr/internal/hsp"
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
		cfg := config.Application()
		f, _ := library.SceneFormat(cfg.VideoRules, vd, cfg.CorrectVerticalStereo)
		source, scene := library.ProfileSourceFor(id, f, libraryService.HasProfile, func() string {
			return libraryService.StudioProfile(ctx, vd, &f)
		})
		switch {
		case source == library.ProfileOwn:
			serveStored(w, r, libraryService.ProfilePath(scene))
		case source == library.ProfileStudio || source == library.ProfileRule:
			serveBorrowed(w, r, libraryService.ProfilePath(scene), vd, f, generate)
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

// serveBorrowed serves the profile stored at path, learned from another
// scene, for vd: its geometry with vd's own id, title, duration, resume
// position, tags, A-B range and rating (see hsp.Profile.ForScene), so a
// sibling scene does not open with the other scene's name or at its
// resume position. When the stored file or the scene's own profile cannot
// be decoded or generated the stored bytes are served as they are.
func serveBorrowed(w http.ResponseWriter, r *http.Request, path string, vd *library.VideoData, f library.Format, generate Generator) {
	ctx := r.Context()
	data, err := os.ReadFile(path)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", vd.Id()).Msg("Failed to read the borrowed HereSphere profile")
		http.NotFound(w, r)
		return
	}
	rebased, err := rebase(r, data, vd, f, generate)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", vd.Id()).Msg("Serving the borrowed HereSphere profile as stored")
		rebased = data
	}
	setHeaders(w)
	_, _ = w.Write(rebased)
}

var errNoGenerator = errors.New("no profile generator")

// rebase re-encodes the profile data, learned from another scene, for vd.
func rebase(r *http.Request, data []byte, vd *library.VideoData, f library.Format, generate Generator) ([]byte, error) {
	if generate == nil {
		return nil, errNoGenerator
	}
	borrowed, err := hsp.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode borrowed profile: %w", err)
	}
	ownData, err := generate(r, vd, f)
	if err != nil {
		return nil, fmt.Errorf("generate the scene's profile: %w", err)
	}
	own, err := hsp.Decode(ownData)
	if err != nil {
		return nil, fmt.Errorf("decode the scene's profile: %w", err)
	}
	return hsp.Encode(borrowed.ForScene(own))
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
