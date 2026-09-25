package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/config"
	"stash-vr/internal/hsp"
	"stash-vr/internal/library"
	"stash-vr/internal/logger"
	"stash-vr/internal/stash"
)

const (
	// maxRequestBody caps PUT/POST bodies; the settings documents are tiny.
	maxRequestBody = 1 << 20
	// stashProbeTimeout bounds the connection test.
	stashProbeTimeout = 10 * time.Second
	// maxStashErrorLen caps a message coming from the Stash client.
	maxStashErrorLen = 200
	// randomStripSize is the default count for /api/ui/random.
	randomStripSize = 6
	// maxRandomScenes caps one /random request.
	maxRandomScenes = 24
)

// RandomScene is one card on the Players page strip: its cover served by
// this service and a link to the scene in Stash.
type RandomScene struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Cover string `json:"cover"`
	Stash string `json:"stash"`
}

// randomScenes draws n scenes from the library and shapes them for the
// strip, with covers under baseUrl.
func randomScenes(ctx context.Context, lib *library.Service, baseUrl string, n int) ([]RandomScene, error) {
	vds, err := lib.RandomScenes(ctx, n)
	if err != nil {
		return nil, err
	}
	stashUrl := config.Application().StashGraphQLUrl
	out := make([]RandomScene, len(vds))
	for i, vd := range vds {
		id := vd.Id()
		out[i] = RandomScene{ID: id, Title: vd.Title(), Cover: heatmap.GetCoverUrl(baseUrl, id), Stash: StashSceneUrl(stashUrl, id)}
	}
	return out, nil
}

// ConfigView is the settings as sent to the browser: the API key is replaced
// by a flag.
type ConfigView struct {
	StashGraphQLUrl    string             `json:"stash_graphql_url"`
	StashApiKeySet     bool               `json:"stash_api_key_set"`
	FavoriteTag        string             `json:"favorite_tag"`
	ExcludeSortName    string             `json:"exclude_sort_name"`
	GenerateSummaryIds bool               `json:"generate_summary_ids"`
	HeatmapHeightPx    int                `json:"heatmap_height_px"`
	ForceHTTPS         bool               `json:"force_https"`
	BasePath           string             `json:"base_path"`
	DeovrAutoload      bool               `json:"deovr_autoload"`
	PerformerFacets    bool               `json:"performer_facets"`
	DateLookup         bool               `json:"date_lookup"`
	DateWriteback      bool               `json:"date_writeback"`
	FunscriptIndexPath string             `json:"funscript_index_path"`
	LogLevel           string             `json:"log_level"`
	SmartSectionSize   int                `json:"smart_section_size"`
	AutoStudioMin      int                `json:"auto_studio_min"`
	AutoPerformerMin   int                `json:"auto_performer_min"`
	CoverBadges        config.CoverBadges `json:"cover_badges"`
	ListenAddress      string             `json:"listen_address"`
	ConfigPath         string             `json:"config_path"`
	Filters            []config.Filter    `json:"filters"`
	VideoRules         []config.VideoRule `json:"video_rules"`
}

// configInput is what PUT /config accepts. An empty StashApiKey keeps the
// current key.
type configInput struct {
	StashGraphQLUrl    string            `json:"stash_graphql_url"`
	StashApiKey        string            `json:"stash_api_key"`
	FavoriteTag        string            `json:"favorite_tag"`
	ExcludeSortName    string            `json:"exclude_sort_name"`
	GenerateSummaryIds bool              `json:"generate_summary_ids"`
	HeatmapHeightPx    int               `json:"heatmap_height_px"`
	ForceHTTPS         bool              `json:"force_https"`
	BasePath           *string           `json:"base_path"`
	DeovrAutoload      *bool             `json:"deovr_autoload"`
	PerformerFacets    *bool             `json:"performer_facets"`
	DateLookup         *bool             `json:"date_lookup"`
	DateWriteback      *bool             `json:"date_writeback"`
	FunscriptIndexPath *string           `json:"funscript_index_path"`
	LogLevel           string            `json:"log_level"`
	SmartSectionSize   *int              `json:"smart_section_size"`
	AutoStudioMin      *int              `json:"auto_studio_min"`
	AutoPerformerMin   *int              `json:"auto_performer_min"`
	CoverBadges        *coverBadgesInput `json:"cover_badges"`
}

// coverBadgesInput is the cover_badges object of PUT /config; a missing
// key keeps the current setting.
type coverBadgesInput struct {
	Quality     *bool `json:"quality"`
	Format      *bool `json:"format"`
	Passthrough *bool `json:"passthrough"`
	Duration    *bool `json:"duration"`
	FrameRate   *bool `json:"framerate"`
}

// apply copies the badges in names onto b.
func (in *coverBadgesInput) apply(b *config.CoverBadges) {
	if in == nil {
		return
	}
	if in.Quality != nil {
		b.Quality = *in.Quality
	}
	if in.Format != nil {
		b.Format = *in.Format
	}
	if in.Passthrough != nil {
		b.Passthrough = *in.Passthrough
	}
	if in.Duration != nil {
		b.Duration = *in.Duration
	}
	if in.FrameRate != nil {
		b.FrameRate = *in.FrameRate
	}
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
		BasePath:           cfg.BasePath,
		DeovrAutoload:      cfg.DeovrAutoload,
		PerformerFacets:    cfg.PerformerFacets,
		DateLookup:         cfg.DateLookup,
		DateWriteback:      cfg.DateWriteback,
		FunscriptIndexPath: cfg.FunscriptIndexPath,
		LogLevel:           cfg.LogLevel,
		SmartSectionSize:   cfg.SmartSectionSize,
		AutoStudioMin:      cfg.AutoStudioMin,
		AutoPerformerMin:   cfg.AutoPerformerMin,
		CoverBadges:        cfg.CoverBadges,
		ListenAddress:      cfg.ListenAddress,
		ConfigPath:         config.FilePath(cfg),
		Filters:            cfg.Filters,
		VideoRules:         cfg.VideoRules,
	}
}

type apiHandler struct {
	lib *library.Service
	// writeMu serialises the read-modify-write cycles of the mutating
	// endpoints so two concurrent settings changes cannot interleave.
	writeMu sync.Mutex
	// coverageCache keeps the last format coverage report for a minute.
	coverageCache coverageCache
	// previewScene keeps the default badge preview scene for ten minutes.
	previewScene previewSceneCache
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
	r.Put("/video-rules", h.putVideoRules)
	r.Get("/profiles/{id}", h.getProfile)
	r.Post("/reindex", h.reindex)
	r.Get("/log", h.getLog)
	r.Get("/random", h.getRandom)
	r.Get("/inspect", h.inspect)
	r.Get("/coverage", h.coverage)
	r.Get("/badge-preview", h.badgePreview)
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
	if in.BasePath != nil {
		next.BasePath = *in.BasePath
	}
	if in.DeovrAutoload != nil {
		next.DeovrAutoload = *in.DeovrAutoload
	}
	if in.PerformerFacets != nil {
		next.PerformerFacets = *in.PerformerFacets
	}
	if in.DateLookup != nil {
		next.DateLookup = *in.DateLookup
	}
	if in.DateWriteback != nil {
		next.DateWriteback = *in.DateWriteback
	}
	if in.FunscriptIndexPath != nil {
		next.FunscriptIndexPath = *in.FunscriptIndexPath
	}
	next.LogLevel = in.LogLevel
	if in.SmartSectionSize != nil {
		next.SmartSectionSize = *in.SmartSectionSize
	}
	if in.AutoStudioMin != nil {
		next.AutoStudioMin = *in.AutoStudioMin
	}
	if in.AutoPerformerMin != nil {
		next.AutoPerformerMin = *in.AutoPerformerMin
	}
	in.CoverBadges.apply(&next.CoverBadges)

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
	h.lib.ResetSections()
	writeJson(ctx, w, map[string]any{"filters": saved.Filters})
}

// putVideoRules replaces the rules table. An empty list restores the
// defaults so "Reset to defaults" needs no separate endpoint.
func (h *apiHandler) putVideoRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := internal.UnmarshalBody[[]config.VideoRule](r)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if len(in) == 0 {
		in = config.DefaultVideoRules()
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	cfg := config.Application()
	cfg.VideoRules = in
	saved, err := config.Set(cfg)
	if err != nil {
		writeError(ctx, w, settingsErrorCode(err), err.Error())
		return
	}
	writeJson(ctx, w, map[string]any{"video_rules": saved.VideoRules})
}

func (h *apiHandler) reindex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h.lib.ResetCaches()
	sections, err := h.lib.GetSections(ctx)
	if err != nil {
		writeError(ctx, w, http.StatusBadGateway, err.Error())
		return
	}
	stats := h.lib.StatsSnapshot()
	writeJson(ctx, w, map[string]int{
		"sections": len(sections),
		"links":    stats.Links,
		"scenes":   stats.Scenes,
	})
}

// getLog returns the last lines of the process log: 200 by default, at most
// the 500 the ring keeps.
func (h *apiHandler) getLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n := 200
	if q := r.URL.Query().Get("lines"); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil || v < 1 {
			writeError(ctx, w, http.StatusBadRequest, "lines must be a positive number")
			return
		}
		n = v
	}
	if n > 500 {
		n = 500
	}
	writeJson(ctx, w, map[string]any{"lines": logger.Tail.Lines(n)})
}

// getRandom returns a fresh set of random scenes for the strip: 6 by
// default, at most 24.
func (h *apiHandler) getRandom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n := randomStripSize
	if q := r.URL.Query().Get("n"); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil || v < 1 {
			writeError(ctx, w, http.StatusBadRequest, "n must be a positive number")
			return
		}
		n = v
	}
	if n > maxRandomScenes {
		n = maxRandomScenes
	}
	scenes, err := randomScenes(ctx, h.lib, internal.GetBaseUrl(r), n)
	if err != nil {
		writeError(ctx, w, http.StatusBadGateway, err.Error())
		return
	}
	writeJson(ctx, w, map[string]any{"scenes": scenes})
}

// profileView is a stored HereSphere profile's screen and background
// settings, keyed like the video rule fields they fill in.
type profileView struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	PositionX       *float64 `json:"position_x,omitempty"`
	PositionY       *float64 `json:"position_y,omitempty"`
	PositionZ       *float64 `json:"position_z,omitempty"`
	Pitch           *float64 `json:"pitch,omitempty"`
	Yaw             *float64 `json:"yaw,omitempty"`
	Roll            *float64 `json:"roll,omitempty"`
	ZoomX           *float64 `json:"zoom_x,omitempty"`
	ZoomY           *float64 `json:"zoom_y,omitempty"`
	PanX            *float64 `json:"pan_x,omitempty"`
	PanY            *float64 `json:"pan_y,omitempty"`
	OriginX         *float64 `json:"origin_x,omitempty"`
	OriginY         *float64 `json:"origin_y,omitempty"`
	OriginZ         *float64 `json:"origin_z,omitempty"`
	Background      string   `json:"background,omitempty"`
	BackgroundColor string   `json:"background_color,omitempty"`
	Mask            string   `json:"mask,omitempty"`
}

// getProfile decodes the profile stored for a scene so the setup page can
// copy its screen settings into a rule.
func (h *apiHandler) getProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if !h.lib.HasProfile(id) {
		writeError(ctx, w, http.StatusNotFound, "no profile stored for scene "+id)
		return
	}
	data, err := os.ReadFile(h.lib.ProfilePath(id))
	if err != nil {
		writeError(ctx, w, http.StatusInternalServerError, err.Error())
		return
	}
	p, err := hsp.Decode(data)
	if err != nil {
		writeError(ctx, w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJson(ctx, w, profileValues(id, p))
}

var (
	profileBackgrounds = map[uint8]string{hsp.BackgroundGlobal: "global", hsp.BackgroundColor: "color", hsp.BackgroundPassthrough: "passthrough"}
	profileMasks       = map[uint8]string{hsp.MaskNone: "none", hsp.MaskChromaKey: "chroma", hsp.MaskAlphaPacked: "alpha"}
)

// profileValues takes the first keyframe of each section; a section the
// profile lacks leaves its fields unset.
func profileValues(id string, p *hsp.Profile) profileView {
	v := profileView{ID: id, Title: p.Title}
	if len(p.Alignment) > 0 {
		a := p.Alignment[0]
		v.PositionX, v.PositionY, v.PositionZ = num(a.Position.X), num(a.Position.Y), num(a.Position.Z)
		v.Pitch, v.Yaw, v.Roll = num(a.Rotation.Pitch), num(a.Rotation.Yaw), num(a.Rotation.Roll)
	}
	if len(p.Format) > 0 {
		f := p.Format[0]
		v.ZoomX, v.ZoomY, v.PanX, v.PanY = num(f.Zoom.X), num(f.Zoom.Y), num(f.Pan.X), num(f.Pan.Y)
	}
	if len(p.Origin) > 0 {
		o := p.Origin[0].Origin
		v.OriginX, v.OriginY, v.OriginZ = num(o.X), num(o.Y), num(o.Z)
	}
	if len(p.Environment) > 0 {
		e := p.Environment[0]
		v.Background = profileBackgrounds[e.Background]
		v.Mask = profileMasks[e.Mask]
		if e.Background == hsp.BackgroundColor {
			v.BackgroundColor = e.BackgroundColor.Hex()
		}
	}
	return v
}

// num widens a stored float32 to the shortest float64 that prints the
// same, so 4.7806 does not come back as 4.780600070953369. NaN and
// infinities, which JSON cannot carry, stay unset.
func num(f float32) *float64 {
	if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
		return nil
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return &v
}
