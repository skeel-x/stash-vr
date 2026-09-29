package heresphere

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
	"html/template"
	"net/http"
	"net/url"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/static"
)

func Router(libraryService *library.Service) http.Handler {
	httpHandler := newHttpHandler(libraryService)
	r := chi.NewRouter()
	r.Use(middleware.SetHeader("HereSphere-JSON-Version", "1"))
	r.Post("/", internal.LogRoute("index", httpHandler.indexHandler))
	r.Get("/", internal.LogRoute("static", loadingHandler))

	r.Post("/scan", internal.LogRoute("scan", httpHandler.scanHandler))
	r.Post("/auth", http.NotFound)
	r.Handle("/{videoId}", internal.LogRoute("videoData", internal.LogVideoId(httpHandler.videoDataHandler)))
	r.Post("/events/{videoId}", internal.LogRoute("events", httpHandler.eventsHandler))
	return r
}

// loadingTmpl is the page a browser sees at the library address. HereSphere
// opens the same address and POSTs for the JSON document, so a person only
// ever sees this page; its links carry the base path.
var loadingTmpl = template.Must(template.ParseFS(static.Fs, "loading.gohtml"))

func loadingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := loadingTmpl.Execute(w, struct{ Base string }{Base: internal.GetBasePath(r)}); err != nil {
		log.Ctx(r.Context()).Error().Err(err).Msg("render loading page")
	}
}

func getVideoDataUrl(baseUrl string, id string) string {
	return baseUrl + "/heresphere/" + url.QueryEscape(id)
}

func getEventsUrl(baseUrl string, id string) string {
	return baseUrl + "/heresphere/events/" + url.QueryEscape(id)
}
