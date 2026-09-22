package web

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/build"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/static"
)

var (
	launchTmpl   = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "launch.gohtml"))
	setupTmpl    = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "setup.gohtml"))
	sectionsTmpl = template.Must(template.ParseFS(static.Fs, "layout.gohtml", "sections.gohtml"))
)

var logLevels = []string{"trace", "debug", "info", "warn", "error"}

// FilterRow is one saved filter with its current override, for the Filters page.
type FilterRow struct {
	ID         string
	SourceName string
	Name       string
	Disabled   bool
	Smart      bool
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
}

type pageHandler struct {
	lib *library.Service
}

// Register adds the page routes to r. The top-level router uses this so its
// /* static file server keeps working alongside the pages.
func Register(r chi.Router, lib *library.Service) {
	h := pageHandler{lib: lib}
	r.Get("/", h.players)
	r.Get("/setup", h.setup)
	r.Get("/sections", h.sections)
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
	Register(r, lib)
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
	// The page embeds the keyed sample cover URL for the headset check, so it
	// must not be cached by a browser or an intermediary.
	w.Header().Set("Cache-Control", "no-store")
	data := h.base(r, "Players", "launch")
	data.Status = BuildStatus(r.Context(), h.lib)
	data.Links = LinksFor(r)
	render(w, r, launchTmpl, data)
}

func (h pageHandler) setup(w http.ResponseWriter, r *http.Request) {
	data := h.base(r, "Setup", "setup")
	data.Config = MaskedConfig(config.Application())
	render(w, r, setupTmpl, data)
}

func (h pageHandler) sections(w http.ResponseWriter, r *http.Request) {
	data := h.base(r, "Sections", "sections")
	rows, err := h.lib.SectionRows(r.Context())
	if err != nil {
		data.FilterError = err.Error()
	}
	for _, row := range rows {
		data.FilterRows = append(data.FilterRows, FilterRow{ID: row.ID, SourceName: row.SourceName, Name: row.Name, Disabled: row.Disabled, Smart: row.Smart})
	}
	render(w, r, sectionsTmpl, data)
}
