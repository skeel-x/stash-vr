package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getPage(t *testing.T, h http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		if k == "Host" {
			req.Host = v // net/http reads the host from req.Host, not the header map
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDashboard_RendersPlayerLinksFromForwardedProto(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", map[string]string{"Host": "vr.example", "X-Forwarded-Proto": "https"})

	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{"Connect your player", "https://vr.example/heresphere", "v0.31.1", `href="/settings"`} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	// The keyed sample cover URL is allowed exactly once, inside the
	// data-cover attribute that drives the headset reachability check.
	if n := strings.Count(body, "secret"); n != 1 {
		t.Fatalf("expected the api key exactly once (sample cover), found %d", n)
	}
	if !strings.Contains(body, `data-cover="http://stash:9999/scene/1/screenshot?apikey=secret"`) {
		t.Fatal("expected the keyed sample cover only in the data-cover attribute")
	}
}

func TestDashboard_WarnsAboutPlainHttpForDeoVR(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", map[string]string{"Host": "10.0.0.2:9666"})

	if !strings.Contains(rec.Body.String(), "DeoVR does not load covers over plain HTTP") {
		t.Fatal("expected plain-http warning for DeoVR")
	}
}

func TestSettings_RendersFormWithoutApiKey(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/settings", nil)

	body := rec.Body.String()
	for _, want := range []string{`name="stash_graphql_url"`, `name="stash_api_key"`, `name="log_level"`, "leave blank to keep"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings missing %q", want)
		}
	}
	if strings.Contains(body, "secret") {
		t.Fatal("settings page leaked the api key")
	}
}

func TestFilters_RendersPage(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/filters", nil)

	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Filter overrides") {
		t.Fatalf("expected filters page, got %d", rec.Code)
	}
}
