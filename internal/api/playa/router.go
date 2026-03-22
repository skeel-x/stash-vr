package playa

import (
	"net/http"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"

	"github.com/go-chi/chi/v5"
)

func Router(libraryService *library.Service) http.Handler {
	httpHandler := httpHandler{libraryService: libraryService}
	r := chi.NewRouter()

	r.Get("/version", internal.LogRoute("version", httpHandler.versionHandler))
	r.Get("/config", internal.LogRoute("config", httpHandler.configHandler))
	r.Get("/videos", internal.LogRoute("videos", httpHandler.videosHandler))
	r.Get("/video/{videoId}", internal.LogRoute("video", httpHandler.videoHandler))
	r.Get("/categories", internal.LogRoute("categories", httpHandler.categoriesHandler))
	r.Get("/categories-groups", internal.LogRoute("categoryGroups", httpHandler.categoryGroupsHandler))
	r.Get("/video-statuses", internal.LogRoute("videoStatuses", httpHandler.videoStatusesHandler))
	r.Get("/actors", internal.LogRoute("actors", httpHandler.actorsHandler))
	r.Get("/actor/{actorId}", internal.LogRoute("actor", httpHandler.actorHandler))
	r.Get("/studios", internal.LogRoute("studios", httpHandler.studiosHandler))
	r.Get("/studio/{studioId}", internal.LogRoute("studio", httpHandler.studioHandler))

	return r
}
