package web

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/build"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/static"
)

var (
	dashboardTmpl = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "dashboard.gohtml"))
	settingsTmpl  = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "settings.gohtml"))
	filtersTmpl   = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "filters.gohtml"))
)

var logLevels = []string{"trace", "debug", "info", "warn", "error"}

// FilterRow is one saved filter with its current override, for the Filters page.
type FilterRow struct {
	ID         string
	SourceName string
	Name       string
	Disabled   bool
}

type pageData struct {
	Title       string
	Active      string
	Version     string
	Status      Status
	Links       PlayerLinks
	Config      ConfigView
	LogLevels   []string
	FilterRows  []FilterRow
	FilterError string
}

type pageHandler struct {
	lib *library.Service
}

// Register adds the page routes to r. The top-level router uses this so its
// /* static file server keeps working alongside the pages.
func Register(r chi.Router, lib *library.Service) {
	h := pageHandler{lib: lib}
	r.Get("/", h.dashboard)
	r.Get("/settings", h.settings)
	r.Get("/filters", h.filters)
}

// PagesRouter serves only the HTML pages; used by tests.
func PagesRouter(lib *library.Service) http.Handler {
	r := chi.NewRouter()
	Register(r, lib)
	return r
}

func (h pageHandler) base(title, active string) pageData {
	return pageData{Title: title, Active: active, Version: build.FullVersion(), LogLevels: logLevels}
}

func render(w http.ResponseWriter, r *http.Request, t *template.Template, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Ctx(r.Context()).Error().Err(err).Msg("render page")
	}
}

func (h pageHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	// The page embeds the keyed sample cover URL for the headset check, so it
	// must not be cached by a browser or an intermediary.
	w.Header().Set("Cache-Control", "no-store")
	data := h.base("Dashboard", "dashboard")
	data.Status = BuildStatus(r.Context(), h.lib)
	data.Links = LinksFor(r)
	render(w, r, dashboardTmpl, data)
}

func (h pageHandler) settings(w http.ResponseWriter, r *http.Request) {
	data := h.base("Settings", "settings")
	data.Config = MaskedConfig(config.Application())
	render(w, r, settingsTmpl, data)
}

func (h pageHandler) filters(w http.ResponseWriter, r *http.Request) {
	data := h.base("Filters", "filters")
	rows, err := h.filterRows(r)
	if err != nil {
		data.FilterError = err.Error()
	}
	data.FilterRows = rows
	render(w, r, filtersTmpl, data)
}

// filterRows lists Stash's saved scene filters in the configured order:
// overridden ones first (in override order), then the rest as Stash returns them.
func (h pageHandler) filterRows(r *http.Request) ([]FilterRow, error) {
	resp, err := gql.FindSavedSceneFilters(r.Context(), h.lib.Client())
	if err != nil {
		return nil, err
	}
	sourceNames := make(map[string]string, len(resp.FindSavedFilters))
	for _, sf := range resp.FindSavedFilters {
		sourceNames[sf.Id] = sf.Name
	}
	rows := make([]FilterRow, 0, len(resp.FindSavedFilters))
	seen := map[string]struct{}{}
	for _, cf := range config.Application().Filters {
		src, ok := sourceNames[cf.ID]
		if !ok {
			continue
		}
		name := cf.Name
		if name == "" {
			name = src
		}
		rows = append(rows, FilterRow{ID: cf.ID, SourceName: src, Name: name, Disabled: cf.Disabled})
		seen[cf.ID] = struct{}{}
	}
	for _, sf := range resp.FindSavedFilters {
		if _, ok := seen[sf.Id]; ok {
			continue
		}
		rows = append(rows, FilterRow{ID: sf.Id, SourceName: sf.Name, Name: sf.Name})
	}
	return rows, nil
}
