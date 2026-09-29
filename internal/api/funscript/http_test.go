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

// fixture is the router with scene 7's scripts on disk: clip.funscript
// (Standard, index 0) and clip.ai.funscript (AI, index 1).
type fixture struct {
	h   http.Handler
	dir string
	// keys are the variant keys by index, as the scene document names them.
	keys []string
}

func newFixture(t *testing.T) fixture {
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
	keys := []string{
		library.ScriptVariant{Path: filepath.Join(dir, "clip.funscript")}.Key(),
		library.ScriptVariant{Path: filepath.Join(dir, "clip.ai.funscript")}.Key(),
	}
	r := chi.NewRouter()
	r.Get("/funscript/{videoId}/{n}", Handler(library.NewService(&fakeStash{path: video})))
	return fixture{h: r, dir: dir, keys: keys}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler_ServesVariantBytes(t *testing.T) {
	f := newFixture(t)

	rec := get(f.h, "/funscript/7/1?k="+f.keys[1])

	if rec.Code != 200 || rec.Body.String() != `{"ai":1}` {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Fatalf("cache control %q", cc)
	}
	if rec := get(f.h, "/funscript/7/0?k="+f.keys[0]); rec.Code != 200 || rec.Body.String() != `{"std":1}` {
		t.Fatalf("standard: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandler_NotFoundCases(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name string
		path string
	}{
		{name: "unknown scene", path: "/funscript/999/0?k=" + f.keys[0]},
		{name: "index past the list", path: "/funscript/7/2?k=" + f.keys[0]},
		{name: "negative index", path: "/funscript/7/-1?k=" + f.keys[0]},
		{name: "index not a number", path: "/funscript/7/x?k=" + f.keys[0]},
		{name: "path traversal", path: "/funscript/7/..%2F..%2Fetc?k=" + f.keys[0]},
		{name: "missing key", path: "/funscript/7/1"},
		{name: "empty key", path: "/funscript/7/1?k="},
		{name: "wrong key", path: "/funscript/7/1?k=000000000000"},
		{name: "key of another variant", path: "/funscript/7/1?k=" + f.keys[0]},
		{name: "key truncated", path: "/funscript/7/1?k=" + f.keys[1][:11]},
		{name: "key extended", path: "/funscript/7/1?k=" + f.keys[1] + "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := get(f.h, c.path); rec.Code != 404 {
				t.Errorf("%s: expected 404, got %d", c.path, rec.Code)
			}
		})
	}
}

func TestHandler_ChangedListDoesNotServeAnotherScript(t *testing.T) {
	// The player holds a scene document naming index 1 as the AI script.
	// The standard script is then removed and the list is rediscovered, so
	// index 1 no longer exists and index 0 is now the AI script: the old
	// URL must not serve anything else than what it named.
	f := newFixture(t)
	aiUrl := "/funscript/7/1?k=" + f.keys[1]
	if rec := get(f.h, aiUrl); rec.Code != 200 {
		t.Fatalf("before the change: got %d", rec.Code)
	}

	// A fresh service sees the new list (the old one caches the scan).
	if err := os.Remove(filepath.Join(f.dir, "clip.funscript")); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Get("/funscript/{videoId}/{n}", Handler(library.NewService(&fakeStash{path: filepath.Join(f.dir, "clip.mp4")})))

	if rec := get(r, aiUrl); rec.Code != 404 {
		t.Fatalf("index 1 is gone: expected 404, got %d %q", rec.Code, rec.Body.String())
	}
	// A URL for index 0 with the standard key must not serve the AI script
	// that now sits at index 0.
	if rec := get(r, "/funscript/7/0?k="+f.keys[0]); rec.Code != 404 {
		t.Fatalf("index 0 is another file now: expected 404, got %d %q", rec.Code, rec.Body.String())
	}
	// The AI script's own key still finds it at its new index.
	if rec := get(r, "/funscript/7/0?k="+f.keys[1]); rec.Code != 200 || rec.Body.String() != `{"ai":1}` {
		t.Fatalf("expected the AI script by its key, got %d %q", rec.Code, rec.Body.String())
	}
}
