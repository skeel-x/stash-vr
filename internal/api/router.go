package api

import (
	"net/http"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/api/deovr"
	"stash-vr/internal/api/funscript"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/api/heresphere"
	"stash-vr/internal/api/hsp"
	"stash-vr/internal/api/playa"
	"stash-vr/internal/api/web"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/static"
	"stash-vr/internal/util"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

func Router(libraryService *library.Service) *chi.Mux {
	router := chi.NewRouter()

	// Rendered covers go with the rest of the library caches.
	libraryService.OnReset(coverbadge.ResetCache)

	router.Use(requestLogger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Compress(5, "application/json"))

	//router.Mount("/debug", middleware.Profiler())

	router.Mount("/heresphere", logMod("heresphere", heresphere.Router(libraryService)))
	router.Mount("/deovr", logMod("deovr", deovr.Router(libraryService)))
	router.Mount("/api/playa/v2", logMod("playa", playa.Router(libraryService)))

	router.Get("/cover/{videoId}", logMod("heatmap", heatmap.CoverHandler(libraryService)).ServeHTTP)
	router.Get("/funscript/{videoId}/{n}", logMod("funscript", funscript.Handler(libraryService)).ServeHTTP)
	router.Get("/hsp/scene/{videoId}", logMod("hsp", hsp.Handler(libraryService, heresphere.GenerateProfile)).ServeHTTP)

	router.Mount("/api/ui", logMod("ui", web.ApiRouter(libraryService)))

	web.Register(router, libraryService, deovr.IndexHandler(libraryService))

	router.Get("/*", http.FileServerFS(static.Fs).ServeHTTP)

	return router
}

func logMod(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := log.Ctx(r.Context()).With().Str("mod", value).Logger().WithContext(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme := util.GetScheme(r)
		url := scheme + "://" + config.Redacted(r.Host) + r.RequestURI

		baseLogger := log.Ctx(r.Context()).With().
			Str("method", r.Method).
			Str("url", url).Logger()

		baseLogger.Debug().
			Str("proto", r.Proto).
			Str("user_agent", r.UserAgent()).
			Msg("Incoming request")

		start := time.Now()
		next.ServeHTTP(w, r)

		baseLogger.Trace().
			Dur("ms", time.Since(start)).
			Msg("Request handled")
	})
}
