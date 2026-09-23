package hsp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

func newRouter(t *testing.T) (*library.Service, http.Handler) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", FavoriteTag: "FAVORITE",
		LogLevel: "info", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	lib := library.NewService(nil)
	r := chi.NewRouter()
	r.Get("/hsp/scene/{videoId}", Handler(lib))
	return lib, r
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler_ServesStoredProfile(t *testing.T) {
	lib, h := newRouter(t)
	if err := lib.SaveProfile("7", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	rec := get(h, "/hsp/scene/7")

	if rec.Code != 200 || rec.Body.String() != "\x01\x02\x03" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache control %q", cc)
	}
}

func TestHandler_NotFound(t *testing.T) {
	_, h := newRouter(t)
	for _, p := range []string{"/hsp/scene/8", "/hsp/scene/abc", "/hsp/scene/..%2Fconfig.json"} {
		if rec := get(h, p); rec.Code != 404 {
			t.Errorf("%s: expected 404, got %d", p, rec.Code)
		}
	}
}
