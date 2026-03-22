package playa

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

const defaultPageSize = 50
const maxPageSize = 1000

type httpHandler struct {
	libraryService *library.Service
}

func (h httpHandler) versionHandler(w http.ResponseWriter, req *http.Request) {
	h.writeJSON(req, w, okRsp(apiVersion))
}

func (h httpHandler) configHandler(w http.ResponseWriter, req *http.Request) {
	baseURL := internal.GetBaseUrl(req)
	logo := baseURL + "/icon.png"
	h.writeJSON(req, w, okRsp(buildConfiguration(&logo)))
}

func (h httpHandler) categoriesHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	savedFilters, err := h.libraryService.GetSavedFilterSceneSets(ctx)
	if err != nil {
		h.writeInternalError(ctx, w, err, "failed to load saved filters")
		return
	}
	tags, err := h.libraryService.GetBrowseTags(ctx)
	if err != nil {
		h.writeInternalError(ctx, w, err, "failed to load tags")
		return
	}
	h.writeJSON(req, w, okRsp(buildCategories(savedFilters, tags)))
}

func (h httpHandler) categoryGroupsHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	savedFilters, err := h.libraryService.GetSavedFilterSceneSets(ctx)
	if err != nil {
		h.writeInternalError(ctx, w, err, "failed to load saved filters")
		return
	}
	tags, err := h.libraryService.GetBrowseTags(ctx)
	if err != nil {
		h.writeInternalError(ctx, w, err, "failed to load tags")
		return
	}
	h.writeJSON(req, w, okRsp(buildCategoryGroups(savedFilters, tags)))
}

func (h httpHandler) videoStatusesHandler(w http.ResponseWriter, req *http.Request) {
	h.writeJSON(req, w, okRsp(buildVideoStatuses()))
}

func (h httpHandler) videosHandler(w http.ResponseWriter, req *http.Request) {
	query, handled := parseVideoQuery(req.URL.Query())
	if handled != nil {
		h.writeJSON(req, w, handled)
		return
	}
	page, err := h.buildVideoPage(req.Context(), query)
	if err != nil {
		h.writeInternalError(req.Context(), w, err, "failed to build video page")
		return
	}
	h.writeJSON(req, w, okRsp(page))
}

func (h httpHandler) videoHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	videoID := chi.URLParam(req, "videoId")
	vd, err := h.libraryService.GetScene(ctx, videoID, false)
	if err != nil {
		h.writeInternalError(ctx, w, err, "failed to load video")
		return
	}
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Id == "" {
		h.writeJSON(req, w, notFoundRsp("Video", videoID))
		return
	}
	savedFilters, err := h.libraryService.GetSavedFilterSceneSets(ctx)
	if err != nil {
		h.writeInternalError(ctx, w, err, "failed to load saved filters")
		return
	}
	view := buildVideoView(vd, savedFilters)
	log.Ctx(ctx).Info().Interface("videoView", view).Msg("Serving video details")
	h.writeJSON(req, w, okRsp(view))
}

func (h httpHandler) actorsHandler(w http.ResponseWriter, req *http.Request) {
	pageIndex, pageSize, handled := parsePageParams(req.URL.Query())
	if handled != nil {
		h.writeJSON(req, w, handled)
		return
	}
	order, direction, handled := parseOrderAndDirection(req.URL.Query(), "title", validateActorOrder)
	if handled != nil {
		h.writeJSON(req, w, handled)
		return
	}
	performers, err := h.libraryService.GetPerformers(req.Context())
	if err != nil {
		h.writeInternalError(req.Context(), w, err, "failed to load actors")
		return
	}
	title := strings.TrimSpace(req.URL.Query().Get("title"))
	h.writeJSON(req, w, okRsp(buildActorPage(performers, pageIndex, pageSize, order, direction, title)))
}

func (h httpHandler) actorHandler(w http.ResponseWriter, req *http.Request) {
	actorID, err := parseEntityID(chi.URLParam(req, "actorId"), actorPrefix, "actor")
	if err != nil {
		h.writeJSON(req, w, errorRsp(err.Error()))
		return
	}
	performer, err := h.libraryService.GetPerformer(req.Context(), actorID)
	if err != nil {
		h.writeInternalError(req.Context(), w, err, "failed to load actor")
		return
	}
	if performer == nil {
		h.writeJSON(req, w, notFoundRsp("Actor", chi.URLParam(req, "actorId")))
		return
	}
	h.writeJSON(req, w, okRsp(buildActorView(performer)))
}

func (h httpHandler) studiosHandler(w http.ResponseWriter, req *http.Request) {
	pageIndex, pageSize, handled := parsePageParams(req.URL.Query())
	if handled != nil {
		h.writeJSON(req, w, handled)
		return
	}
	order, direction, handled := parseOrderAndDirection(req.URL.Query(), "title", validateStudioOrder)
	if handled != nil {
		h.writeJSON(req, w, handled)
		return
	}
	studios, err := h.libraryService.GetStudios(req.Context())
	if err != nil {
		h.writeInternalError(req.Context(), w, err, "failed to load studios")
		return
	}
	h.writeJSON(req, w, okRsp(buildStudioPage(studios, pageIndex, pageSize, order, direction)))
}

func (h httpHandler) studioHandler(w http.ResponseWriter, req *http.Request) {
	studioID, err := parseEntityID(chi.URLParam(req, "studioId"), studioPrefix, "studio")
	if err != nil {
		h.writeJSON(req, w, errorRsp(err.Error()))
		return
	}
	studio, err := h.libraryService.GetStudio(req.Context(), studioID)
	if err != nil {
		h.writeInternalError(req.Context(), w, err, "failed to load studio")
		return
	}
	if studio == nil {
		h.writeJSON(req, w, notFoundRsp("Studio", chi.URLParam(req, "studioId")))
		return
	}
	h.writeJSON(req, w, okRsp(buildStudioView(studio)))
}

func (h httpHandler) writeJSON(req *http.Request, w http.ResponseWriter, data any) {
	if err := internal.WriteJson(req.Context(), w, data); err != nil {
		log.Ctx(req.Context()).Error().Err(err).Msg("error writing Playa response")
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (h httpHandler) writeInternalError(ctx context.Context, w http.ResponseWriter, err error, message string) {
	if err != nil {
		log.Ctx(ctx).Error().Err(fmt.Errorf("%s: %w", message, err)).Msg("playa request failed")
	} else {
		log.Ctx(ctx).Error().Msg(message)
	}
	w.WriteHeader(http.StatusInternalServerError)
}

func parsePageParams(values url.Values) (int, int, *Rsp) {
	pageIndex := 0
	pageSize := defaultPageSize
	if raw := strings.TrimSpace(values.Get("page-index")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, 0, &Rsp{Status: Status{Code: statusError, Message: "invalid page-index"}}
		}
		pageIndex = parsed
	}
	if raw := strings.TrimSpace(values.Get("page-size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > maxPageSize {
			return 0, 0, &Rsp{Status: Status{Code: statusError, Message: "invalid page-size"}}
		}
		pageSize = parsed
	}
	return pageIndex, pageSize, nil
}

func parseOrderAndDirection(values url.Values, defaultOrder string, validate func(string) error) (string, string, *Rsp) {
	order := strings.TrimSpace(values.Get("order"))
	if order == "" {
		order = defaultOrder
	}
	if err := validate(order); err != nil {
		return "", "", &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	direction := strings.TrimSpace(values.Get("direction"))
	if direction == "" {
		direction = "asc"
	}
	if direction != "asc" && direction != "desc" {
		return "", "", &Rsp{Status: Status{Code: statusError, Message: "unsupported direction: " + direction}}
	}
	return order, direction, nil
}

func parseVideoQuery(values url.Values) (videoQuery, *Rsp) {
	pageIndex, pageSize, handled := parsePageParams(values)
	if handled != nil {
		return videoQuery{}, handled
	}
	order, direction, handled := parseOrderAndDirection(values, "title", validateVideoOrder)
	if handled != nil {
		return videoQuery{}, handled
	}
	includedCategories, err := parseCategoryList(values.Get("included-categories"))
	if err != nil {
		return videoQuery{}, &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	excludedCategories, err := parseCategoryList(values.Get("excluded-categories"))
	if err != nil {
		return videoQuery{}, &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	includedStatuses, err := parseStatusList(values.Get("included-statuses"))
	if err != nil {
		return videoQuery{}, &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	excludedStatuses, err := parseStatusList(values.Get("excluded-statuses"))
	if err != nil {
		return videoQuery{}, &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	actorID, err := parseEntityID(strings.TrimSpace(values.Get("actor")), actorPrefix, "actor")
	if err != nil {
		return videoQuery{}, &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	studioID, err := parseEntityID(strings.TrimSpace(values.Get("studio")), studioPrefix, "studio")
	if err != nil {
		return videoQuery{}, &Rsp{Status: Status{Code: statusError, Message: err.Error()}}
	}
	return videoQuery{
		PageIndex:          pageIndex,
		PageSize:           pageSize,
		Order:              order,
		Direction:          direction,
		Title:              strings.TrimSpace(values.Get("title")),
		ActorID:            actorID,
		StudioID:           studioID,
		IncludedCategories: includedCategories,
		ExcludedCategories: excludedCategories,
		IncludedStatuses:   includedStatuses,
		ExcludedStatuses:   excludedStatuses,
	}, nil
}
