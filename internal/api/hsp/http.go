// Package hsp serves the HereSphere profiles stored per scene.
package hsp

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"stash-vr/internal/library"
)

func Handler(libraryService *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "videoId")
		if !libraryService.HasProfile(id) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, libraryService.ProfilePath(id))
	}
}
