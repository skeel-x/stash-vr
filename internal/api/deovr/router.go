package deovr

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
)

func Router(libraryService *library.Service) http.Handler {
	httpHandler := httpHandler{libraryService}
	r := chi.NewRouter()

	r.Get("/", internal.LogRoute("index", httpHandler.indexHandler))
	r.Get("/{videoId}", internal.LogRoute("videoData", internal.LogVideoId(httpHandler.videoDataHandler)))
	return r
}

// IndexHandler serves the DeoVR library document; the web front page uses
// it to answer DeoVR's browser directly.
func IndexHandler(libraryService *library.Service) http.HandlerFunc {
	return httpHandler{libraryService}.indexHandler
}

func getVideoDataUrl(baseUrl string, id string) string {
	return baseUrl + "/deovr/" + id
}
