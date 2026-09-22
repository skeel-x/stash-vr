package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
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

func TestPlayers_RendersLaunchLinksFromForwardedProto(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", map[string]string{"Host": "vr.example", "X-Forwarded-Proto": "https"})

	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"Open your library",
		`href="https://vr.example/heresphere"`,
		`href="https://vr.example/deovr"`,
		`data-copy="https://vr.example/heresphere"`,
		`data-copy="https://vr.example"`,
		"Connected to Stash v0.31.1",
		`href="/setup"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("players page missing %q", want)
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
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", cc)
	}
}

func TestPlayers_WarnsAboutPlainHttpForDeoVR(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", map[string]string{"Host": "10.0.0.2:9666"})

	if !strings.Contains(rec.Body.String(), "Covers stay blank in DeoVR over plain http") {
		t.Fatal("expected plain-http note for DeoVR")
	}
}

func TestPlayers_BlockedWhenStashUnreachable(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{versionErr: errors.New("dial tcp: connection refused")})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", nil)

	body := rec.Body.String()
	if !strings.Contains(body, "Players can't see a library yet") {
		t.Fatal("expected the blocked block when Stash is unreachable")
	}
	if strings.Contains(body, "Open your library") {
		t.Fatal("launch strips must be hidden when Stash is unreachable")
	}
}

func TestSetup_RendersFormWithoutApiKey(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/setup", nil)

	body := rec.Body.String()
	for _, want := range []string{`name="stash_graphql_url"`, `name="stash_api_key"`, `name="log_level"`, "leave blank to keep"} {
		if !strings.Contains(body, want) {
			t.Errorf("setup missing %q", want)
		}
	}
	if strings.Contains(body, "secret") {
		t.Fatal("setup page leaked the api key")
	}
}

func TestSections_RendersPage(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/sections", nil)

	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Reset to Stash order") {
		t.Fatalf("expected sections page, got %d", rec.Code)
	}
	if !strings.Contains(body, "Continue watching") {
		t.Fatal("expected disabled smart sections to still be listed")
	}
	if !strings.Contains(body, ">smart<") {
		t.Fatal("expected the smart badge")
	}
}

func TestPlayers_ShowsRejectedKeyWhenUnauthorized(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{versionErr: &graphql.HTTPError{StatusCode: 401}})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", nil)

	body := rec.Body.String()
	if !strings.Contains(body, "Stash rejected the API key.") {
		t.Fatal("expected the rejected-key health line")
	}
	if !strings.Contains(body, "Players can't see a library yet") {
		t.Fatal("expected the blocked block when the key is rejected")
	}
}

func TestPlayers_PlayaBandIsNotALink(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	body := getPage(t, h, "/", nil).Body.String()

	i := strings.Index(body, "Add this server in Playa")
	if i < 0 {
		t.Fatal("expected the Playa band")
	}
	// The band element that contains the action text must be a div, not an anchor.
	start := strings.LastIndex(body[:i], `class="launch`)
	if start < 0 || !strings.HasPrefix(body[strings.LastIndex(body[:start], "<"):], "<div") {
		t.Fatal("expected the Playa band to be a div, not a link")
	}
}

func TestOldRoutes_Redirect(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	for old, want := range map[string]string{"/settings": "/setup", "/filters": "/sections"} {
		rec := getPage(t, h, old, nil)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != want {
			t.Errorf("%s: expected 301 to %s, got %d %q", old, want, rec.Code, rec.Header().Get("Location"))
		}
	}

	rec := getPage(t, h, "/settings", map[string]string{"X-Forwarded-Prefix": "/p"})
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/p/setup" {
		t.Errorf("/settings behind prefix: expected 301 to /p/setup, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPlayers_LinksCarryForwardedPrefix(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	body := getPage(t, h, "/", map[string]string{"Host": "vr.example", "X-Forwarded-Proto": "https", "X-Forwarded-Prefix": "/stashvr"}).Body.String()

	for _, want := range []string{`href="https://vr.example/stashvr/heresphere"`, `href="/stashvr/app.css"`, `href="/stashvr/setup"`, `data-base="/stashvr"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}
