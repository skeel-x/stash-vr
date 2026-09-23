package web

import (
	"html/template"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/deovr"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/build"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/logger"
	"stash-vr/internal/static"
)

var (
	launchTmpl   = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "launch.gohtml"))
	setupTmpl    = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "setup.gohtml"))
	sectionsTmpl = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "sections.gohtml"))
	logTmpl      = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "log.gohtml"))
)

// logPageLines is how many lines the Log page shows on load and on refresh.
const logPageLines = 300

var logLevels = []string{"trace", "debug", "info", "warn", "error"}

// FilterRow is one saved filter with its current override, for the Filters page.
type FilterRow struct {
	ID         string
	SourceName string
	Name       string
	Disabled   bool
	Smart      bool
	HiddenIn   []string
}

// ShowIn reports whether the section is listed in player: it must be
// enabled and not hidden for that player.
func (r FilterRow) ShowIn(player string) bool {
	return !r.Disabled && !slices.Contains(r.HiddenIn, player)
}

// ProfileOption is a stored HereSphere profile offered to a video rule; the
// title is empty when the scene could not be looked up.
type ProfileOption struct {
	ID    string
	Title string
}

type pageData struct {
	Title       string
	Base        string
	Active      string
	Version     string
	Status      Status
	Links       PlayerLinks
	Config      ConfigView
	LogLevels   []string
	FilterRows  []FilterRow
	FilterError string
	VideoRules  []config.VideoRule
	Profiles    []ProfileOption
	Projections []string
	Stereos     []string
	Lenses      []string
	LogLines    []string
	Random      []RandomScene
}

// ruleRow is what the rule-row template renders: one rule with the option
// lists its selects offer.
type ruleRow struct {
	Rule        config.VideoRule
	Projections []string
	Stereos     []string
	Lenses      []string
	Profiles    []ProfileOption
}

// Row binds a rule to the page's option lists for the rule-row template.
func (d pageData) Row(r config.VideoRule) ruleRow {
	return ruleRow{Rule: r, Projections: d.Projections, Stereos: d.Stereos, Lenses: d.Lenses, Profiles: d.Profiles}
}

// EmptyRow is the blank row the "Add rule" button clones.
func (d pageData) EmptyRow() ruleRow {
	return d.Row(config.VideoRule{})
}

type pageHandler struct {
	lib *library.Service
	// deovrIndex answers DeoVR's browser on the front page with the library
	// document instead of the Players page.
	deovrIndex http.HandlerFunc
}

// Register adds the page routes to r. The top-level router uses this so its
// /* static file server keeps working alongside the pages.
func Register(r chi.Router, lib *library.Service, deovrIndex http.HandlerFunc) {
	h := pageHandler{lib: lib, deovrIndex: deovrIndex}
	r.Get("/", h.players)
	r.Get("/setup", h.setup)
	r.Get("/sections", h.sections)
	r.Get("/log", h.logPage)
	r.Get("/settings", redirectTo("/setup"))
	r.Get("/filters", redirectTo("/sections"))
}

// redirectTo sends the client to path under the base path the request was
// served at, computed per request because the prefix may come from a header.
func redirectTo(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.GetBasePath(r)+path, http.StatusMovedPermanently)
	}
}

// PagesRouter serves only the HTML pages; used by tests.
func PagesRouter(lib *library.Service) http.Handler {
	r := chi.NewRouter()
	Register(r, lib, deovr.IndexHandler(lib))
	return r
}

func (h pageHandler) base(r *http.Request, title, active string) pageData {
	return pageData{Title: title, Base: internal.GetBasePath(r), Active: active, Version: build.FullVersion(), LogLevels: logLevels}
}

func render(w http.ResponseWriter, r *http.Request, t *template.Template, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Ctx(r.Context()).Error().Err(err).Msg("render page")
	}
}

func (h pageHandler) players(w http.ResponseWriter, r *http.Request) {
	if h.deovrIndex != nil && config.Application().DeovrAutoload && strings.Contains(strings.ToLower(r.UserAgent()), "deovr") {
		h.deovrIndex(w, r)
		return
	}
	// The page embeds the keyed sample cover URL for the headset check, so it
	// must not be cached by a browser or an intermediary.
	w.Header().Set("Cache-Control", "no-store")
	data := h.base(r, "Players", "launch")
	data.Status = BuildStatus(r.Context(), h.lib)
	data.Links = LinksFor(r)
	if data.Status.Connection == "ok" {
		// A failed draw only leaves the strip empty; the page still renders.
		random, err := randomScenes(r.Context(), h.lib, internal.GetBaseUrl(r), randomStripSize)
		if err != nil {
			log.Ctx(r.Context()).Debug().Err(err).Msg("Random strip: no scenes")
		}
		data.Random = random
	}
	render(w, r, launchTmpl, data)
}

func (h pageHandler) setup(w http.ResponseWriter, r *http.Request) {
	data := h.base(r, "Setup", "setup")
	cfg := config.Application()
	data.Config = MaskedConfig(cfg)
	data.VideoRules = cfg.VideoRules
	data.Projections = []string{"equirectangular", "equirectangular360", "fisheye", "cubemap", "equiangularCubemap", "perspective"}
	data.Stereos = []string{"mono", "sbs", "tb"}
	data.Lenses = []string{"MKX200", "MKX220", "VRCA220"}
	// A scene that cannot be looked up is still listed by its id.
	for _, id := range h.lib.ListProfiles() {
		title := ""
		if vd, err := h.lib.GetScene(r.Context(), id, false); err == nil {
			title = vd.Title()
		}
		data.Profiles = append(data.Profiles, ProfileOption{ID: id, Title: title})
	}
	render(w, r, setupTmpl, data)
}

func (h pageHandler) sections(w http.ResponseWriter, r *http.Request) {
	data := h.base(r, "Sections", "sections")
	rows, err := h.lib.SectionRows(r.Context())
	if err != nil {
		data.FilterError = err.Error()
	}
	for _, row := range rows {
		data.FilterRows = append(data.FilterRows, FilterRow{ID: row.ID, SourceName: row.SourceName, Name: row.Name, Disabled: row.Disabled, Smart: row.Smart, HiddenIn: row.HiddenIn})
	}
	render(w, r, sectionsTmpl, data)
}

func (h pageHandler) logPage(w http.ResponseWriter, r *http.Request) {
	data := h.base(r, "Log", "log")
	data.LogLines = logger.Tail.Lines(logPageLines)
	render(w, r, logTmpl, data)
}
