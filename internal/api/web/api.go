package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
)

// ConfigView is the settings as sent to the browser: the API key is replaced
// by a flag.
type ConfigView struct {
	StashGraphQLUrl    string          `json:"stash_graphql_url"`
	StashApiKeySet     bool            `json:"stash_api_key_set"`
	FavoriteTag        string          `json:"favorite_tag"`
	ExcludeSortName    string          `json:"exclude_sort_name"`
	GenerateSummaryIds bool            `json:"generate_summary_ids"`
	HeatmapHeightPx    int             `json:"heatmap_height_px"`
	ForceHTTPS         bool            `json:"force_https"`
	LogLevel           string          `json:"log_level"`
	ListenAddress      string          `json:"listen_address"`
	ConfigPath         string          `json:"config_path"`
	Filters            []config.Filter `json:"filters"`
}

// configInput is what PUT /config accepts. An empty StashApiKey keeps the
// current key.
type configInput struct {
	StashGraphQLUrl    string `json:"stash_graphql_url"`
	StashApiKey        string `json:"stash_api_key"`
	FavoriteTag        string `json:"favorite_tag"`
	ExcludeSortName    string `json:"exclude_sort_name"`
	GenerateSummaryIds bool   `json:"generate_summary_ids"`
	HeatmapHeightPx    int    `json:"heatmap_height_px"`
	ForceHTTPS         bool   `json:"force_https"`
	LogLevel           string `json:"log_level"`
}

type testInput struct {
	StashGraphQLUrl string `json:"stash_graphql_url"`
	StashApiKey     string `json:"stash_api_key"`
}

type testResult struct {
	Ok           bool   `json:"ok"`
	StashVersion string `json:"stash_version,omitempty"`
	Error        string `json:"error,omitempty"`
}

func MaskedConfig(cfg config.ApplicationConfig) ConfigView {
	return ConfigView{
		StashGraphQLUrl:    cfg.StashGraphQLUrl,
		StashApiKeySet:     cfg.StashApiKey != "",
		FavoriteTag:        cfg.FavoriteTag,
		ExcludeSortName:    cfg.ExcludeSortName,
		GenerateSummaryIds: cfg.GenerateSummaryIds,
		HeatmapHeightPx:    cfg.HeatmapHeightPx,
		ForceHTTPS:         cfg.ForceHTTPS,
		LogLevel:           cfg.LogLevel,
		ListenAddress:      cfg.ListenAddress,
		ConfigPath:         config.FilePath(cfg),
		Filters:            cfg.Filters,
	}
}

type apiHandler struct {
	lib *library.Service
}

// ApiRouter serves the JSON API used by the pages. Mounted at /api/ui.
func ApiRouter(lib *library.Service) http.Handler {
	h := apiHandler{lib: lib}
	r := chi.NewRouter()
	r.Use(requireJsonContentType)
	r.Get("/status", h.status)
	r.Get("/config", h.getConfig)
	r.Put("/config", h.putConfig)
	r.Post("/config/test", h.testConfig)
	r.Put("/filters", h.putFilters)
	r.Post("/reindex", h.reindex)
	return r
}

// requireJsonContentType rejects PUT/POST requests that are not
// application/json, so a cross-origin browser request must preflight (and
// fail) rather than land as a simple request.
func requireJsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut || r.Method == http.MethodPost {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeError(r.Context(), w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// hostChanged reports whether next names a different Stash host than
// current, so callers can require the API key to be resupplied rather than
// silently sending the stored key to a different host.
func hostChanged(current, next string) bool {
	cu, err1 := url.Parse(current)
	nu, err2 := url.Parse(next)
	if err1 != nil || err2 != nil {
		return true
	}
	return !strings.EqualFold(cu.Host, nu.Host)
}

func writeError(ctx context.Context, w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	if err := internal.WriteJson(ctx, w, map[string]string{"error": msg}); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("write error response")
	}
}

func writeJson(ctx context.Context, w http.ResponseWriter, v any) {
	if err := internal.WriteJson(ctx, w, v); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("write response")
	}
}

func (h apiHandler) status(w http.ResponseWriter, r *http.Request) {
	writeJson(r.Context(), w, BuildStatus(r.Context(), h.lib))
}

func (h apiHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJson(r.Context(), w, MaskedConfig(config.Application()))
}

func (h apiHandler) putConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[configInput](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	prev := config.Application()
	if hostChanged(prev.StashGraphQLUrl, in.StashGraphQLUrl) && in.StashApiKey == "" {
		writeError(ctx, w, http.StatusBadRequest, "api key required when changing the Stash host")
		return
	}
	next := prev
	next.StashGraphQLUrl = in.StashGraphQLUrl
	if in.StashApiKey != "" {
		next.StashApiKey = in.StashApiKey
	}
	next.FavoriteTag = in.FavoriteTag
	next.ExcludeSortName = in.ExcludeSortName
	next.GenerateSummaryIds = in.GenerateSummaryIds
	next.HeatmapHeightPx = in.HeatmapHeightPx
	next.ForceHTTPS = in.ForceHTTPS
	next.LogLevel = in.LogLevel

	saved, err := config.Set(next)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	ApplyChanges(prev, saved, h.lib)
	writeJson(ctx, w, MaskedConfig(saved))
}

func (h apiHandler) testConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[testInput](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if hostChanged(config.Application().StashGraphQLUrl, in.StashGraphQLUrl) && in.StashApiKey == "" {
		writeJson(ctx, w, testResult{Ok: false, Error: "api key required when testing a different Stash host"})
		return
	}
	key := in.StashApiKey
	if key == "" {
		key = config.Application().StashApiKey
	}
	probe := config.Application()
	probe.StashGraphQLUrl = in.StashGraphQLUrl
	if err := config.Validate(probe); err != nil {
		writeJson(ctx, w, testResult{Ok: false, Error: err.Error()})
		return
	}
	version, err := stash.GetVersion(ctx, stash.NewClient(in.StashGraphQLUrl, key))
	if err != nil {
		writeJson(ctx, w, testResult{Ok: false, Error: err.Error()})
		return
	}
	writeJson(ctx, w, testResult{Ok: true, StashVersion: version})
}

func (h apiHandler) putFilters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[[]config.Filter](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	cfg := config.Application()
	cfg.Filters = in
	saved, err := config.Set(cfg)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	writeJson(ctx, w, map[string]any{"filters": saved.Filters})
}

func (h apiHandler) reindex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h.lib.ResetCaches()
	sections, err := h.lib.GetSections(ctx)
	if err != nil {
		writeError(ctx, w, http.StatusBadGateway, err.Error())
		return
	}
	writeJson(ctx, w, map[string]int{
		"sections": len(sections),
		"links":    h.lib.Stats.Links,
		"scenes":   h.lib.Stats.Scenes,
	})
}
