package funscript

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// fakeStash knows one scene, id 7, whose video lives at path. Like the
// real Stash it answers FindScenes for any other id with no scenes.
type fakeStash struct{ path string }

func (f *fakeStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	if req.OpName != "FindScenes" {
		return json.Unmarshal([]byte(`{}`), resp.Data)
	}
	if in, ok := req.Variables.(interface{ GetScene_ids() []int }); !ok || !slices.Contains(in.GetScene_ids(), 7) {
		return json.Unmarshal([]byte(`{"findScenes":{"scenes":[]}}`), resp.Data)
	}
	scene := map[string]any{
		"id": "7", "title": "Seven", "created_at": "2024-01-01T00:00:00Z", "tags": []any{},
		"files": []map[string]any{{"basename": filepath.Base(f.path), "duration": 100, "path": f.path, "height": 1080, "video_codec": "h264"}},
	}
	b, _ := json.Marshal(map[string]any{"findScenes": map[string]any{"scenes": []any{scene}}})
	return json.Unmarshal(b, resp.Data)
}

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", FavoriteTag: "FAVORITE",
		LogLevel: "info", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	for name, content := range map[string]string{"clip.mp4": "video", "clip.funscript": `{"std":1}`, "clip.ai.funscript": `{"ai":1}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := chi.NewRouter()
	r.Get("/funscript/{videoId}/{n}", Handler(library.NewService(&fakeStash{path: video})))
	return r
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler_ServesVariantBytes(t *testing.T) {
	h := newRouter(t)

	rec := get(h, "/funscript/7/1")

	if rec.Code != 200 || rec.Body.String() != `{"ai":1}` {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Fatalf("cache control %q", cc)
	}
}

func TestHandler_NotFoundCases(t *testing.T) {
	h := newRouter(t)
	for _, p := range []string{"/funscript/999/0", "/funscript/7/2", "/funscript/7/-1", "/funscript/7/x", "/funscript/7/..%2F..%2Fetc"} {
		if rec := get(h, p); rec.Code != 404 {
			t.Errorf("%s: expected 404, got %d", p, rec.Code)
		}
	}
}
