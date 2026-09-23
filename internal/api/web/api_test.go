package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// fakeStash answers generated queries by operation name.
type fakeStash struct {
	versionErr error
	mu         sync.Mutex
	calls      map[string]int
}

func (f *fakeStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[req.OpName]++
	f.mu.Unlock()
	var payload string
	switch req.OpName {
	case "Version":
		if f.versionErr != nil {
			return f.versionErr
		}
		payload = `{"version":{"version":"v0.31.1"}}`
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllSceneIds":
		payload = `{"findScenes":{"scenes":[{"id":"1"},{"id":"2"}]}}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindScenes":
		payload = `{"findScenes":{"scenes":[{"id":"1","title":"One","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"one.mp4","duration":100,"path":"/one.mp4","height":1080,"video_codec":"h264"}],"paths":{"screenshot":"http://stash:9999/scene/1/screenshot","stream":"http://stash:9999/scene/1/stream"},"tags":[]},{"id":"2","title":"Two","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"two.mp4","duration":100,"path":"/two.mp4","height":1080,"video_codec":"h264"}],"paths":{"screenshot":"http://stash:9999/scene/2/screenshot","stream":"http://stash:9999/scene/2/stream"},"tags":[]}]}}`
	case "FindSampleSceneCover":
		payload = `{"findScenes":{"scenes":[{"paths":{"screenshot":"http://stash:9999/scene/1/screenshot"}}]}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func newEnv(t *testing.T, stash *fakeStash) (*library.Service, http.Handler) {
	t.Helper()
	seed := config.ApplicationConfig{
		ListenAddress:    ":9666",
		StashGraphQLUrl:  "http://stash:9999/graphql",
		StashApiKey:      "secret",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		DeovrAutoload:    true,
		ConfigPath:       t.TempDir(),
		// The fake Stash always answers FindSceneIdsByFilter with 0 scenes, so
		// the default smart sections (enabled out of the box) would otherwise
		// crowd out the "All" fallback these tests assert on; disable them to
		// keep the status/reindex counts about the fallback, not smart sections.
		Filters: []config.Filter{
			{ID: "smart:continue", Disabled: true},
			{ID: "smart:recent", Disabled: true},
			{ID: "smart:random", Disabled: true},
		},
	}
	if err := config.Load(seed); err != nil {
		t.Fatal(err)
	}
	lib := library.NewService(stash)
	return lib, ApiRouter(lib)
}

func do(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("invalid json body: %v: %s", err, rec.Body.String())
		}
	}
	return rec, out
}

func TestStatus_ReportsOkWithCounts(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodGet, "/status", nil)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if out["connection"] != "ok" || out["stash_version"] != "v0.31.1" {
		t.Fatalf("expected ok connection, got %v", out)
	}
	if out["sections"].(float64) != 1 || out["links"].(float64) != 2 || out["scenes"].(float64) != 2 {
		t.Fatalf("expected counts 1/2/2, got %v", out)
	}
	if out["api_key_set"] != true {
		t.Fatalf("expected api_key_set, got %v", out)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("api key leaked in status response")
	}
	if strings.Contains(rec.Body.String(), "sample_cover_url") {
		t.Fatal("sample cover url must not be serialised")
	}
}

func TestBuildStatus_KeepsSampleCoverForPages(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})

	s := BuildStatus(context.Background(), lib)

	if s.SampleCoverUrl == "" || !strings.Contains(s.SampleCoverUrl, "apikey=") {
		t.Fatalf("expected sample cover url with apikey, got %q", s.SampleCoverUrl)
	}
}

func TestStatus_ReportsUnauthorizedOn401(t *testing.T) {
	_, h := newEnv(t, &fakeStash{versionErr: &graphql.HTTPError{StatusCode: 401}})

	_, out := do(t, h, http.MethodGet, "/status", nil)

	if out["connection"] != "unauthorized" {
		t.Fatalf("expected unauthorized, got %v", out["connection"])
	}
}

func TestGetConfig_NeverExposesApiKey(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodGet, "/config", nil)

	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("api key leaked in config response")
	}
	if out["stash_api_key_set"] != true {
		t.Fatalf("expected stash_api_key_set true, got %v", out)
	}
}

func TestPutConfig_BlankKeyKeepsCurrentAndPersists(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "LOVED", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
	}

	rec, _ := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	cfg := config.Application()
	if cfg.StashApiKey != "secret" || cfg.FavoriteTag != "LOVED" {
		t.Fatalf("expected key kept and tag updated, got %#v", cfg)
	}
	data, _ := os.ReadFile(config.FilePath(cfg))
	if !strings.Contains(string(data), "LOVED") {
		t.Fatal("expected change persisted to config.json")
	}
}

func TestPutConfig_PersistsSmartSectionSize(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 120,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || out["smart_section_size"].(float64) != 120 {
		t.Fatalf("expected 200 with smart_section_size 120, got %d %v", rec.Code, out)
	}
	if config.Application().SmartSectionSize != 120 {
		t.Fatal("expected the size to be stored")
	}
}

func TestPutConfig_PersistsDeovrAutoload(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
		"deovr_autoload": false,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || out["deovr_autoload"] != false {
		t.Fatalf("expected 200 with deovr_autoload false, got %d %v", rec.Code, out)
	}
	if config.Application().DeovrAutoload {
		t.Fatal("expected autoload off to be stored")
	}
	data, _ := os.ReadFile(config.FilePath(config.Application()))
	if !strings.Contains(string(data), `"deovr_autoload": false`) {
		t.Fatalf("expected deovr_autoload persisted to config.json, got %s", data)
	}

	delete(body, "deovr_autoload")
	rec, _ = do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("expected 200 without the field, got %d %s", rec.Code, rec.Body.String())
	}
	if config.Application().DeovrAutoload {
		t.Fatal("expected autoload kept off when the field is missing")
	}
}

func TestPutConfig_MissingSmartSectionSizeKeepsCurrent(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info",
	}

	rec, _ := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("expected 200 without the field, got %d %s", rec.Code, rec.Body.String())
	}
	if config.Application().SmartSectionSize != 50 {
		t.Fatalf("expected size kept at 50, got %d", config.Application().SmartSectionSize)
	}
}

func TestPutConfig_NewUrlSwapsLibraryClient(t *testing.T) {
	lib, h := newEnv(t, &fakeStash{})
	before := lib.Client()
	body := map[string]any{
		"stash_graphql_url": "http://elsewhere:9999/graphql", "stash_api_key": "newkey",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
	}

	rec, _ := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if lib.Client() == before {
		t.Fatal("expected the library client to be replaced after a URL change")
	}
	if config.Application().StashApiKey != "newkey" {
		t.Fatalf("expected new api key to be stored, got %q", config.Application().StashApiKey)
	}
}

func TestPutConfig_NewHostWithoutKeyIs400(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://elsewhere:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if msg, _ := out["error"].(string); rec.Code != 400 || msg == "" {
		t.Fatalf("expected 400 with a non-empty error string, got %d %v", rec.Code, out)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected a JSON error body, got Content-Type %q", ct)
	}
	if config.Application().StashGraphQLUrl != "http://stash:9999/graphql" {
		t.Fatal("store must be unchanged")
	}
}

func TestTestConfig_NewHostWithoutKeyIsRefused(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodPost, "/config/test", map[string]any{
		"stash_graphql_url": "http://elsewhere:9999/graphql", "stash_api_key": "",
	})

	if rec.Code != 200 || out["ok"] != false {
		t.Fatalf("expected ok=false, got %d %v", rec.Code, out)
	}
	errStr, _ := out["error"].(string)
	if !strings.Contains(errStr, "api key") {
		t.Fatalf("expected error mentioning api key, got %v", out)
	}
}

func TestMutation_RequiresJsonContentType(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected a JSON error body, got Content-Type %q", ct)
	}
}

func TestPutConfig_InvalidIs400AndUnchanged(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "nope", "stash_api_key": "", "favorite_tag": "FAVORITE",
		"exclude_sort_name": "hidden", "heatmap_height_px": 0, "log_level": "info", "smart_section_size": 50,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if msg, _ := out["error"].(string); rec.Code != 400 || msg == "" {
		t.Fatalf("expected 400 with a non-empty error string, got %d %v", rec.Code, out)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected a JSON error body, got Content-Type %q", ct)
	}
	if config.Application().StashGraphQLUrl != "http://stash:9999/graphql" {
		t.Fatal("store must be unchanged")
	}
}

func TestTestConfig_DoesNotPersist(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodPost, "/config/test", map[string]any{
		"stash_graphql_url": "http://127.0.0.1:1/graphql", "stash_api_key": "",
	})

	if msg, _ := out["error"].(string); rec.Code != 200 || out["ok"] != false || msg == "" {
		t.Fatalf("expected ok=false with a non-empty error string, got %d %v", rec.Code, out)
	}
	if config.Application().StashGraphQLUrl != "http://stash:9999/graphql" {
		t.Fatal("test must not persist the url")
	}
}

func TestPutFilters_PersistsOrder(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, _ := do(t, h, http.MethodPut, "/filters", []map[string]any{
		{"id": "2", "name": "", "disabled": false},
		{"id": "1", "name": "Renamed", "disabled": true},
	})

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	f := config.Application().Filters
	if len(f) != 2 || f[0].ID != "2" || f[1].Name != "Renamed" || !f[1].Disabled {
		t.Fatalf("unexpected filters %#v", f)
	}
}

func TestReindex_ReturnsCounts(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodPost, "/reindex", nil)

	if rec.Code != 200 || out["sections"].(float64) != 1 || out["scenes"].(float64) != 2 {
		t.Fatalf("unexpected reindex response %d %v", rec.Code, out)
	}
}

func TestTestConfig_DoesNotEchoResponseBody(t *testing.T) {
	// A fake HTTP server that answers 401 with a secret-looking body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"private":"do-not-leak"}`))
	}))
	t.Cleanup(srv.Close)
	_, h := newEnv(t, &fakeStash{})

	_, out := do(t, h, http.MethodPost, "/config/test", map[string]any{
		"stash_graphql_url": srv.URL + "/graphql", "stash_api_key": "k",
	})

	if out["ok"] != false || strings.Contains(out["error"].(string), "do-not-leak") {
		t.Fatalf("response body must not be echoed, got %v", out)
	}
	if !strings.Contains(out["error"].(string), "401") {
		t.Fatalf("expected the status code in the message, got %v", out["error"])
	}
}

// A body that is not a GraphQL response is carried verbatim inside
// genqlient's HTTPError, so this is the case that actually leaks.
func TestTestConfig_DoesNotEchoNonJsonResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("<html>internal proxy says: do-not-leak</html>"))
	}))
	t.Cleanup(srv.Close)
	_, h := newEnv(t, &fakeStash{})

	_, out := do(t, h, http.MethodPost, "/config/test", map[string]any{
		"stash_graphql_url": srv.URL + "/graphql", "stash_api_key": "k",
	})

	msg, _ := out["error"].(string)
	if out["ok"] != false || strings.Contains(msg, "do-not-leak") {
		t.Fatalf("response body must not be echoed, got %v", out)
	}
	if !strings.Contains(msg, "401") {
		t.Fatalf("expected the status code in the message, got %q", msg)
	}
}

func TestPutConfig_InvalidUrlReportsValidationNotHostRule(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{"stash_graphql_url": "nope", "stash_api_key": "", "favorite_tag": "FAVORITE",
		"exclude_sort_name": "hidden", "heatmap_height_px": 0, "log_level": "info", "smart_section_size": 50}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 400 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected JSON 400, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "absolute http(s) URL") {
		t.Fatalf("expected URL validation message, got %q", msg)
	}
}

func TestHostChanged_Table(t *testing.T) {
	cases := []struct {
		cur, next string
		want      bool
	}{
		{"https://stash:9999/graphql", "https://stash:9999/graphql", false},
		{"https://stash:9999/graphql", "https://STASH:9999/graphql", false},
		{"https://stash:9999/graphql", "https://stash:9998/graphql", true},
		{"https://stash:9999/graphql", "http://stash:9999/graphql", true}, // scheme downgrade
		{"http://stash:9999/graphql", "https://stash:9999/graphql", false},
		{"https://stash:9999/graphql", "https://[::1]:9999/graphql", true},
		{"https://stash:9999/graphql", "nope", true},
	}
	for _, c := range cases {
		if got := hostChanged(c.cur, c.next); got != c.want {
			t.Errorf("hostChanged(%q, %q) = %v, want %v", c.cur, c.next, got, c.want)
		}
	}
}

func TestPutConfig_LogLevelChangeSetsGlobalLevel(t *testing.T) {
	t.Cleanup(func() { zerolog.SetGlobalLevel(zerolog.InfoLevel) })
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "debug", "smart_section_size": 50,
	}

	rec, _ := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Fatalf("expected the global level to be debug, got %v", zerolog.GlobalLevel())
	}

	body["log_level"] = "info"
	if rec, _ := do(t, h, http.MethodPut, "/config", body); rec.Code != 200 {
		t.Fatalf("restore status %d: %s", rec.Code, rec.Body.String())
	}
	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Fatalf("expected the global level back to info, got %v", zerolog.GlobalLevel())
	}
}

func TestPutConfig_PersistsFunscriptIndexPath(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
		"funscript_index_path": "/opt/stash/funscript_index.sqlite",
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || out["funscript_index_path"] != "/opt/stash/funscript_index.sqlite" {
		t.Fatalf("expected 200 with the path echoed, got %d %v", rec.Code, out)
	}
	if config.Application().FunscriptIndexPath != "/opt/stash/funscript_index.sqlite" {
		t.Fatal("expected the path to be stored")
	}

	delete(body, "funscript_index_path")
	rec, _ = do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || config.Application().FunscriptIndexPath != "/opt/stash/funscript_index.sqlite" {
		t.Fatalf("expected the path kept when the field is missing, got %d %q", rec.Code, config.Application().FunscriptIndexPath)
	}

	body["funscript_index_path"] = "relative.sqlite"
	rec, _ = do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 400 {
		t.Fatalf("expected 400 for a relative path, got %d %s", rec.Code, rec.Body.String())
	}
}
