package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
)

const (
	// maxRequestBody caps PUT/POST bodies; the settings documents are tiny.
	maxRequestBody = 1 << 20
	// stashProbeTimeout bounds the connection test.
	stashProbeTimeout = 10 * time.Second
	// maxStashErrorLen caps a message coming from the Stash client.
	maxStashErrorLen = 200
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
	SmartSectionSize   int             `json:"smart_section_size"`
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
	SmartSectionSize   *int   `json:"smart_section_size"`
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
		SmartSectionSize:   cfg.SmartSectionSize,
		ListenAddress:      cfg.ListenAddress,
		ConfigPath:         config.FilePath(cfg),
		Filters:            cfg.Filters,
	}
}

type apiHandler struct {
	lib *library.Service
	// writeMu serialises the read-modify-write cycles of the mutating
	// endpoints so two concurrent settings changes cannot interleave.
	writeMu sync.Mutex
}

// ApiRouter serves the JSON API used by the pages. Mounted at /api/ui.
func ApiRouter(lib *library.Service) http.Handler {
	h := &apiHandler{lib: lib}
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
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

// hostChanged reports whether next names a different Stash host than
// current, or downgrades the same host from https to http, so callers can
// require the API key to be resupplied rather than silently sending the
// stored key to a different host or over an unencrypted connection.
func hostChanged(current, next string) bool {
	cu, err1 := url.Parse(current)
	nu, err2 := url.Parse(next)
	if err1 != nil || err2 != nil {
		return true
	}
	if !strings.EqualFold(cu.Host, nu.Host) {
		return true
	}
	return strings.EqualFold(cu.Scheme, "https") && !strings.EqualFold(nu.Scheme, "https")
}

// describeStashError turns an error from the Stash client into a message that
// is safe to show in the UI: a response body from an arbitrary host is never
// echoed, only its status code.
func describeStashError(err error) string {
	var httpErr *graphql.HTTPError
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("Stash returned HTTP %d", httpErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %d seconds", int(stashProbeTimeout.Seconds()))
	}
	msg := err.Error()
	if len(msg) > maxStashErrorLen {
		msg = strings.ToValidUTF8(msg[:maxStashErrorLen], "") + "..."
	}
	return msg
}

// settingsErrorCode maps a config.Set failure to a status code: a rejected
// value is the caller's fault, a failed write is ours.
func settingsErrorCode(err error) int {
	if errors.Is(err, config.ErrInvalid) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func writeError(ctx context.Context, w http.ResponseWriter, code int, msg string) {
	body, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("encode error response")
		body = []byte(`{"error":"internal error"}`)
	}
	// The headers must be set before WriteHeader or they are dropped.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	if _, err := w.Write(body); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("write error response")
	}
}

func writeJson(ctx context.Context, w http.ResponseWriter, v any) {
	if err := internal.WriteJson(ctx, w, v); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("write response")
	}
}

func (h *apiHandler) status(w http.ResponseWriter, r *http.Request) {
	writeJson(r.Context(), w, BuildStatus(r.Context(), h.lib))
}

func (h *apiHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJson(r.Context(), w, MaskedConfig(config.Application()))
}

func (h *apiHandler) putConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[configInput](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	prev := config.Application()
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
	if in.SmartSectionSize != nil {
		next.SmartSectionSize = *in.SmartSectionSize
	}

	// Validate before the host rule so an unusable URL is reported as such
	// rather than as a missing API key.
	if err := config.Validate(next); err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	if hostChanged(prev.StashGraphQLUrl, in.StashGraphQLUrl) && in.StashApiKey == "" {
		writeError(ctx, w, http.StatusBadRequest, "api key required when changing the Stash host")
		return
	}

	saved, err := config.Set(next)
	if err != nil {
		writeError(ctx, w, settingsErrorCode(err), err.Error())
		return
	}
	ApplyChanges(prev, saved, h.lib)
	writeJson(ctx, w, MaskedConfig(saved))
}

func (h *apiHandler) testConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[testInput](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	cur := config.Application()

	// Validate before the host rule so an unusable URL is reported as such
	// rather than as a missing API key.
	probe := cur
	probe.StashGraphQLUrl = in.StashGraphQLUrl
	if err := config.Validate(probe); err != nil {
		writeJson(ctx, w, testResult{Ok: false, Error: err.Error()})
		return
	}
	if hostChanged(cur.StashGraphQLUrl, in.StashGraphQLUrl) && in.StashApiKey == "" {
		writeJson(ctx, w, testResult{Ok: false, Error: "api key required when testing a different Stash host"})
		return
	}
	key := in.StashApiKey
	if key == "" {
		key = cur.StashApiKey
	}

	probeCtx, cancel := context.WithTimeout(ctx, stashProbeTimeout)
	defer cancel()
	version, err := stash.GetVersion(probeCtx, stash.NewClient(in.StashGraphQLUrl, key))
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Stash connection test failed")
		writeJson(ctx, w, testResult{Ok: false, Error: describeStashError(err)})
		return
	}
	writeJson(ctx, w, testResult{Ok: true, StashVersion: version})
}

func (h *apiHandler) putFilters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[[]config.Filter](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	cfg := config.Application()
	cfg.Filters = in
	saved, err := config.Set(cfg)
	if err != nil {
		writeError(ctx, w, settingsErrorCode(err), err.Error())
		return
	}
	writeJson(ctx, w, map[string]any{"filters": saved.Filters})
}

func (h *apiHandler) reindex(w http.ResponseWriter, r *http.Request) {
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
