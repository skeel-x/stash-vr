// Package funscript serves the script variants library.Service discovers
// on disk. The client names a variant by its index in the list, never by
// a path.
package funscript

import (
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"stash-vr/internal/library"
)

func Handler(libraryService *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		n, err := strconv.Atoi(chi.URLParam(r, "n"))
		if err != nil || n < 0 {
			http.NotFound(w, r)
			return
		}
		variants := libraryService.ScriptVariants(ctx, chi.URLParam(r, "videoId"))
		if n >= len(variants) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(variants[n].Path)
		if err != nil {
			log.Ctx(ctx).Debug().Err(err).Msg("Script variant vanished")
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, "", info.ModTime(), f)
	}
}
