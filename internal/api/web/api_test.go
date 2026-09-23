package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog"
	"stash-vr/internal/config"
	hspfile "stash-vr/internal/hsp"
	"stash-vr/internal/library"
	"stash-vr/internal/logger"
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
	case "FindSceneGroupings":
		payload = `{"findScenes":{"scenes":[{"id":"1","studio":{"id":"7","name":"Studio Seven"},"performers":[]},{"id":"2","studio":{"id":"7","name":"Studio Seven"},"performers":[]}]}}`
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
		PerformerFacets:  true,
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

func TestPutConfig_PersistsPerformerFacets(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
		"performer_facets": false,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || out["performer_facets"] != false {
		t.Fatalf("expected 200 with performer_facets false, got %d %v", rec.Code, out)
	}
	if config.Application().PerformerFacets {
		t.Fatal("expected facets off to be stored")
	}
	data, _ := os.ReadFile(config.FilePath(config.Application()))
	if !strings.Contains(string(data), `"performer_facets": false`) {
		t.Fatalf("expected performer_facets persisted to config.json, got %s", data)
	}

	delete(body, "performer_facets")
	rec, _ = do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("expected 200 without the field, got %d %s", rec.Code, rec.Body.String())
	}
	if config.Application().PerformerFacets {
		t.Fatal("expected facets kept off when the field is missing")
	}
}

func TestPutConfig_PersistsDateSettings(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
		"date_lookup": false, "date_writeback": true,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || out["date_lookup"] != false || out["date_writeback"] != true {
		t.Fatalf("expected 200 with date_lookup false and date_writeback true, got %d %v", rec.Code, out)
	}
	if config.Application().DateLookup || !config.Application().DateWriteback {
		t.Fatal("expected the date settings stored")
	}
	data, _ := os.ReadFile(config.FilePath(config.Application()))
	if !strings.Contains(string(data), `"date_lookup": false`) || !strings.Contains(string(data), `"date_writeback": true`) {
		t.Fatalf("expected the date settings persisted to config.json, got %s", data)
	}

	delete(body, "date_lookup")
	delete(body, "date_writeback")
	rec, _ = do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 {
		t.Fatalf("expected 200 without the fields, got %d %s", rec.Code, rec.Body.String())
	}
	if config.Application().DateLookup || !config.Application().DateWriteback {
		t.Fatal("expected the date settings kept when the fields are missing")
	}
}

func (f *fakeStash) callCount(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func TestPutConfig_PersistsAutoSectionMinsAndRebuildsIndex(t *testing.T) {
	stash := &fakeStash{}
	lib, h := newEnv(t, stash)
	if _, err := lib.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := stash.callCount("FindSceneGroupings"); n != 0 {
		t.Fatalf("thresholds default to 0: expected no grouping query, got %d", n)
	}
	body := map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "stash_api_key": "",
		"favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "generate_summary_ids": false,
		"heatmap_height_px": 0, "force_https": false, "log_level": "info", "smart_section_size": 50,
		"auto_studio_min": 2, "auto_performer_min": 30,
	}

	rec, out := do(t, h, http.MethodPut, "/config", body)

	if rec.Code != 200 || out["auto_studio_min"] != float64(2) || out["auto_performer_min"] != float64(30) {
		t.Fatalf("expected 200 with both thresholds, got %d %v", rec.Code, out)
	}
	if cfg := config.Application(); cfg.AutoStudioMin != 2 || cfg.AutoPerformerMin != 30 {
		t.Fatal("expected the thresholds stored")
	}
	sections, err := lib.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := stash.callCount("FindSceneGroupings"); n != 1 {
		t.Fatalf("changing a threshold must rebuild the index: expected one grouping query, got %d", n)
	}
	found := false
	for _, s := range sections {
		found = found || (s.ID == "studio:7" && s.Name == "Studio Seven")
	}
	if !found {
		t.Fatalf("expected the studio section after the change, got %+v", sections)
	}

	delete(body, "auto_studio_min")
	delete(body, "auto_performer_min")
	if rec, _ := do(t, h, http.MethodPut, "/config", body); rec.Code != 200 {
		t.Fatalf("expected 200 without the fields, got %d %s", rec.Code, rec.Body.String())
	}
	if cfg := config.Application(); cfg.AutoStudioMin != 2 || cfg.AutoPerformerMin != 30 {
		t.Fatal("expected the thresholds kept when the fields are missing")
	}

	body["auto_studio_min"] = -1
	if rec, _ := do(t, h, http.MethodPut, "/config", body); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a negative threshold, got %d", rec.Code)
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

func TestPutVideoRules_RoundTripAndValidation(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	rules := []map[string]any{
		{"tag": "DOME", "projection": "equirectangular", "stereo": "sbs"},
		{"tag": "Passthrough", "passthrough": true, "profile": "11649"},
	}

	rec, out := do(t, h, http.MethodPut, "/video-rules", rules)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	saved, _ := out["video_rules"].([]any)
	if len(saved) != 2 || config.Application().VideoRules[1].Profile != "11649" {
		t.Fatalf("expected 2 rules stored, got %v / %+v", out, config.Application().VideoRules)
	}
	_, cfgOut := do(t, h, http.MethodGet, "/config", nil)
	if got, _ := cfgOut["video_rules"].([]any); len(got) != 2 {
		t.Fatalf("expected GET /config to list the rules, got %v", cfgOut["video_rules"])
	}

	// A settings save must not touch the rules table.
	rec, _ = do(t, h, http.MethodPut, "/config", map[string]any{
		"stash_graphql_url": "http://stash:9999/graphql", "favorite_tag": "FAVORITE", "exclude_sort_name": "hidden", "log_level": "info",
	})
	if rec.Code != 200 || len(config.Application().VideoRules) != 2 {
		t.Fatalf("expected PUT /config to keep the 2 rules, got %d %+v", rec.Code, config.Application().VideoRules)
	}

	rec, _ = do(t, h, http.MethodPut, "/video-rules", []map[string]any{{"tag": "X", "projection": "dome"}})
	if rec.Code != 400 {
		t.Fatalf("expected 400 for a bad projection, got %d", rec.Code)
	}
	if len(config.Application().VideoRules) != 2 {
		t.Fatal("a rejected PUT must not change the stored rules")
	}

	rec, out = do(t, h, http.MethodPut, "/video-rules", []any{})
	if rec.Code != 200 || len(config.Application().VideoRules) != 13 {
		t.Fatalf("expected an empty PUT to restore the %d defaults, got %d %v", 13, rec.Code, out)
	}
}

func TestGetLog_ReturnsTailWithClamp(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})
	for i := 0; i < 3; i++ {
		_, _ = fmt.Fprintf(logger.Tail, "tail line %d\n", i)
	}

	_, out := do(t, h, http.MethodGet, "/log?lines=2", nil)
	lines, _ := out["lines"].([]any)
	if len(lines) != 2 || lines[1] != "tail line 2" {
		t.Fatalf("got %v", out)
	}
	rec, out := do(t, h, http.MethodGet, "/log?lines=9999", nil)
	if rec.Code != 200 || len(out["lines"].([]any)) > 500 {
		t.Fatalf("expected clamp to 500, got %d %v", rec.Code, out)
	}
	rec, _ = do(t, h, http.MethodGet, "/log?lines=abc", nil)
	if rec.Code != 400 {
		t.Fatalf("expected 400 for a bad count, got %d", rec.Code)
	}
}

func TestPutFilters_PersistsHiddenIn(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodPut, "/filters", []map[string]any{
		{"id": "1", "name": "", "disabled": false, "hidden_in": []string{"deovr"}},
	})

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	f := config.Application().Filters
	if len(f) != 1 || len(f[0].HiddenIn) != 1 || f[0].HiddenIn[0] != "deovr" {
		t.Fatalf("unexpected filters %#v", f)
	}
	filters, _ := out["filters"].([]any)
	if len(filters) != 1 {
		t.Fatalf("expected the saved filters echoed back, got %v", out)
	}
	hidden, _ := filters[0].(map[string]any)["hidden_in"].([]any)
	if len(hidden) != 1 || hidden[0] != "deovr" {
		t.Fatalf("expected hidden_in echoed back, got %v", out)
	}

	rec, _ = do(t, h, http.MethodPut, "/filters", []map[string]any{
		{"id": "1", "hidden_in": []string{"vlc"}},
	})
	if rec.Code != 400 {
		t.Fatalf("expected 400 for an unknown player, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRandom_ReturnsScenesFromIndex(t *testing.T) {
	_, h := newEnv(t, &fakeStash{})

	rec, out := do(t, h, http.MethodGet, "/random?n=2", nil)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	scenes, _ := out["scenes"].([]any)
	if len(scenes) != 2 {
		t.Fatalf("expected 2 scenes, got %v", out)
	}
	for _, s := range scenes {
		m, _ := s.(map[string]any)
		cover, _ := m["cover"].(string)
		if !strings.HasSuffix(cover, "/cover/1") && !strings.HasSuffix(cover, "/cover/2") {
			t.Errorf("unexpected cover %q", cover)
		}
		if !strings.HasPrefix(cover, "http://example.com/") {
			t.Errorf("cover should be absolute under the request base, got %q", cover)
		}
		if stash, _ := m["stash"].(string); !strings.HasPrefix(stash, "http://stash:9999/scenes/") {
			t.Errorf("unexpected stash link %q", stash)
		}
		if title, _ := m["title"].(string); title == "" {
			t.Errorf("expected a title, got %v", m)
		}
		if id, _ := m["id"].(string); id == "" {
			t.Errorf("expected an id, got %v", m)
		}
	}

	rec, out = do(t, h, http.MethodGet, "/random", nil)
	if rec.Code != 200 || len(out["scenes"].([]any)) != 2 {
		t.Fatalf("expected the default count to return every indexed scene, got %d %v", rec.Code, out)
	}
	rec, out = do(t, h, http.MethodGet, "/random?n=999", nil)
	if rec.Code != 200 || len(out["scenes"].([]any)) != 2 {
		t.Fatalf("expected a large count to be clamped, got %d %v", rec.Code, out)
	}
	for _, bad := range []string{"abc", "0", "-1"} {
		rec, _ = do(t, h, http.MethodGet, "/random?n="+bad, nil)
		if rec.Code != 400 {
			t.Fatalf("expected 400 for n=%s, got %d", bad, rec.Code)
		}
	}
}

func TestGetProfile_ReturnsDecodedValues(t *testing.T) {
	lib, h := newEnv(t, &fakeStash{})
	p := hspfile.Default()
	p.Title = "Captured"
	p.Alignment[0].Position = hspfile.Vec3{X: 0, Y: 4.7806, Z: -1.5}
	p.Alignment[0].Rotation = hspfile.Rotator{Pitch: 2, Yaw: -3, Roll: 0.5}
	p.Format[0].Zoom = hspfile.Vec2{X: 1.25, Y: 1}
	p.Format[0].Pan = hspfile.Vec2{X: 0.1, Y: 0}
	p.Origin[0].Origin = hspfile.Vec3{X: -0.0627, Y: -0.0194, Z: 0.05}
	p.Environment[0].Background = hspfile.BackgroundColor
	p.Environment[0].BackgroundColor = hspfile.ColorFromHex("#1a2b3c")
	p.Environment[0].Mask = hspfile.MaskAlphaPacked
	data, err := hspfile.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SaveProfile("11649", data); err != nil {
		t.Fatal(err)
	}

	rec, out := do(t, h, http.MethodGet, "/profiles/11649", nil)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	want := map[string]any{
		"id": "11649", "title": "Captured",
		"position_x": 0.0, "position_y": 4.7806, "position_z": -1.5, "pitch": 2.0, "yaw": -3.0, "roll": 0.5,
		"zoom_x": 1.25, "zoom_y": 1.0, "pan_x": 0.1, "pan_y": 0.0, "origin_x": -0.0627, "origin_y": -0.0194, "origin_z": 0.05,
		"background": "color", "background_color": "#1a2b3c", "mask": "alpha",
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s = %v (%T), want %v", k, out[k], out[k], v)
		}
	}
}

func TestGetProfile_Errors(t *testing.T) {
	lib, h := newEnv(t, &fakeStash{})
	if err := lib.SaveProfile("12", []byte("not a profile")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/profiles/99", "/profiles/abc", "/profiles/..%2Fconfig.json"} {
		if rec, _ := do(t, h, http.MethodGet, p, nil); rec.Code != 404 {
			t.Errorf("%s: expected 404, got %d", p, rec.Code)
		}
	}
	if rec, out := do(t, h, http.MethodGet, "/profiles/12", nil); rec.Code != http.StatusUnprocessableEntity || out["error"] == nil {
		t.Fatalf("unreadable profile: %d %v", rec.Code, out)
	}
}

func TestProfileValues_EnumsAndMissingSections(t *testing.T) {
	p := hspfile.Default()
	cases := []struct {
		bg, mask         uint8
		wantBg, wantMask string
	}{
		{hspfile.BackgroundGlobal, hspfile.MaskNone, "global", "none"},
		{hspfile.BackgroundPassthrough, hspfile.MaskChromaKey, "passthrough", "chroma"},
		{9, 9, "", ""},
	}
	for _, c := range cases {
		p.Environment[0].Background, p.Environment[0].Mask = c.bg, c.mask
		v := profileValues("1", p)
		if v.Background != c.wantBg || v.Mask != c.wantMask {
			t.Errorf("%d/%d: got %q/%q", c.bg, c.mask, v.Background, v.Mask)
		}
		if v.BackgroundColor != "" {
			t.Errorf("colour only applies to a colour background, got %q", v.BackgroundColor)
		}
	}
	p.Alignment[0].Position.X = float32(math.NaN())
	if v := profileValues("1", p); v.PositionX != nil || v.PositionY == nil {
		t.Fatalf("NaN must stay unset: %+v", v)
	}
	empty := profileValues("1", &hspfile.Profile{})
	if empty.PositionX != nil || empty.ZoomX != nil || empty.OriginX != nil || empty.Background != "" {
		t.Fatalf("missing sections must stay unset: %+v", empty)
	}
}
