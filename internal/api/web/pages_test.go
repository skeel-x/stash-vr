package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
	"stash-vr/internal/logger"
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

func TestSetup_RendersDateSettingsAndStats(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/setup", nil)

	body := rec.Body.String()
	for _, want := range []string{`name="date_lookup"`, `name="date_writeback"`, "Release dates: 0 found, 0 missing, 0 unchecked"} {
		if !strings.Contains(body, want) {
			t.Errorf("setup missing %q", want)
		}
	}
}

func TestSetup_RendersAutoSectionInputs(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	body := getPage(t, h, "/setup", nil).Body.String()

	for _, want := range []string{`name="auto_studio_min"`, `name="auto_performer_min"`, "Studio sections from N scenes", "Performer sections from N scenes", "0 turns it off. Up to 50 of each, largest first."} {
		if !strings.Contains(body, want) {
			t.Errorf("setup missing %q", want)
		}
	}
}

func TestSections_ShowsAutoBadge(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	cfg := config.Application()
	cfg.AutoStudioMin = 2
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	h := PagesRouter(lib)

	body := getPage(t, h, "/sections", nil).Body.String()

	if !strings.Contains(body, `data-id="studio:7"`) || !strings.Contains(body, "Studio Seven") {
		t.Fatal("expected the generated studio row")
	}
	if !strings.Contains(body, ">auto<") {
		t.Fatal("expected the auto badge")
	}
	// The generated row is the only one shown in HereSphere, so it is the landing row.
	if strings.Count(body, `class="badge landing" title="HereSphere opens on this section">Opens first`) != 1 {
		t.Fatal("expected exactly one visible landing badge")
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
	for _, want := range []string{`class="show-heresphere"`, `class="show-deovr"`, `class="show-playa"`} {
		if !strings.Contains(body, want) {
			t.Errorf("expected a per-player checkbox %s", want)
		}
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

func TestPlayers_ShowsRandomStrip(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	body := getPage(t, h, "/", map[string]string{"Host": "vr.example", "X-Forwarded-Proto": "https"}).Body.String()

	for _, want := range []string{`id="random-item"`, `id="shuffle"`, `src="https://vr.example/cover/`, `href="http://stash:9999/scenes/`} {
		if !strings.Contains(body, want) {
			t.Errorf("players page missing %q", want)
		}
	}

	lib, _ = newEnv(t, &fakeStash{versionErr: errors.New("down")})
	body = getPage(t, PagesRouter(lib), "/", nil).Body.String()
	if strings.Contains(body, `id="random-item"`) {
		t.Fatal("the random strip must be hidden when Stash is unreachable")
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

func TestPlayers_DeoVRUserAgentGetsLibraryJson(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", map[string]string{"User-Agent": "Mozilla/5.0 DeoVR/1.6 Quest"})

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON for DeoVR, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"scenes"`) {
		t.Fatal("expected the DeoVR library document")
	}
}

func TestPlayers_DeoVRUserAgentGetsHtmlWhenAutoloadOff(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	cfg := config.Application()
	cfg.DeovrAutoload = false
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	h := PagesRouter(lib)

	rec := getPage(t, h, "/", map[string]string{"User-Agent": "DeoVR"})

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected HTML with autoload off, got %q", ct)
	}
}

func TestSetup_RendersVideoRulesAndProfiles(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	if err := lib.SaveProfile("11649", []byte("x")); err != nil {
		t.Fatal(err)
	}
	h := PagesRouter(lib)

	body := getPage(t, h, "/setup", nil).Body.String()

	for _, want := range []string{"Video rules", `data-rule`, `value="DOME"`, `value="Augmented Reality"`, `<option value="11649"`, "Profiles stored: 1", `id="save-rules"`, `id="reset-rules"`, `id="add-rule"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestLog_RendersPage(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	_, _ = fmt.Fprintln(logger.Tail, "hello from the log")
	h := PagesRouter(lib)

	body := getPage(t, h, "/log", nil).Body.String()
	for _, want := range []string{"hello from the log", `id="log-lines"`, `id="log-refresh"`, `id="log-auto"`, `id="log-level"`, `id="log-filter"`, `href="/log"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestSections_MarksLandingRow(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	cfg := config.Application()
	cfg.Filters = append(cfg.Filters, config.Filter{ID: "smart:toprated"})
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	h := PagesRouter(lib)

	body := getPage(t, h, "/sections", nil).Body.String()

	shown := strings.Count(body, `class="badge landing" title="HereSphere opens on this section">Opens first`)
	hidden := strings.Count(body, `class="badge landing" title="HereSphere opens on this section" hidden>`)
	if shown != 1 || hidden < 1 {
		t.Fatalf("expected exactly one visible landing badge and the rest hidden, got %d visible, %d hidden", shown, hidden)
	}
}

func TestSetup_RendersScreenAndBackgroundGroup(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	if err := lib.SaveProfile("11649", []byte("x")); err != nil {
		t.Fatal(err)
	}
	y, zoom := 4.78, 1.5
	cfg := config.Application()
	cfg.VideoRules = []config.VideoRule{
		{Tag: "Passthrough", Passthrough: true, PositionY: &y, ZoomX: &zoom, Background: "color", BackgroundColor: "#1a2b3c", Mask: "alpha"},
		{Tag: "DOME", Projection: "equirectangular"},
	}
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	h := PagesRouter(lib)

	body := getPage(t, h, "/setup", nil).Body.String()

	for _, want := range []string{
		"Screen and background", `<details class="rule-screen" open>`, `<details class="rule-screen">`,
		`data-key="position_y" type="number"`, `value="4.78"`, `data-key="zoom_x"`, `value="1.5"`, `data-key="origin_z"`,
		`<option value="color" selected>`, `class="background-color" type="color" value="#1a2b3c"`, `<option value="alpha" selected>`,
		`class="copy-profile"`, "Copy from a saved profile", "Values use HereSphere's own units.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Unset geometry renders blank, not 0.
	if strings.Contains(body, `data-key="position_x" type="number" step="any" value="0"`) {
		t.Error("unset position_x must render blank")
	}
}

func TestSetup_RendersEyeSwapAndForceMono(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	yes, no := true, false
	cfg := config.Application()
	cfg.VideoRules = []config.VideoRule{{Tag: "RL", EyeSwap: &yes}, {Tag: "M", ForceMono: &no}}
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	body := getPage(t, PagesRouter(lib), "/setup", nil).Body.String()
	for _, want := range []string{
		`Eye swap<select class="eye-swap"><option value="">unchanged</option><option value="on" selected>on</option><option value="off">off</option></select>`,
		`Force mono<select class="force-mono"><option value="">unchanged</option><option value="on">on</option><option value="off" selected>off</option></select>`,
		`Force mono<select class="force-mono"><option value="">unchanged</option><option value="on">on</option><option value="off">off</option></select>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestSetup_OffersMissingDefaultRules(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	cfg := config.Application()
	cfg.VideoRules = []config.VideoRule{{Tag: "dome", Stereo: "tb"}}
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	body := getPage(t, PagesRouter(lib), "/setup", nil).Body.String()
	if !strings.Contains(body, `id="add-defaults"`) || !strings.Contains(body, "Add missing default rules") {
		t.Fatal("missing the Add missing default rules button")
	}
	start := strings.Index(body, `<template id="default-rules">`)
	if start < 0 {
		t.Fatal("missing the default rules template")
	}
	tmpl := body[start : start+strings.Index(body[start:], "</template>")]
	for _, r := range config.DefaultVideoRules() {
		if !strings.Contains(tmpl, `value="`+template.HTMLEscapeString(r.Tag)+`"`) {
			t.Errorf("default rule %q not offered", r.Tag)
		}
	}
	if got := strings.Count(tmpl, "data-rule"); got != len(config.DefaultVideoRules()) {
		t.Fatalf("expected %d default rule cards, got %d", len(config.DefaultVideoRules()), got)
	}
}

func TestSetup_OffersRulePresets(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	body := getPage(t, PagesRouter(lib), "/setup", nil).Body.String()
	for _, want := range []string{
		`<select id="add-preset"`, `<option value="">Add a preset</option>`,
		`<option value="0">RF52 190</option>`, `<option value="1">MKX200 200</option>`, `<option value="2">MKX220 220</option>`,
		`<option value="3">VRCA220 220</option>`, `<option value="4">Passthrough</option>`, `<option value="5">Flat 2D</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	start := strings.Index(body, `<template id="preset-rules">`)
	if start < 0 {
		t.Fatal("missing the preset template")
	}
	tmpl := body[start : start+strings.Index(body[start:], "</template>")]
	if got := strings.Count(tmpl, "data-rule"); got != 6 {
		t.Fatalf("expected 6 preset cards, got %d", got)
	}
	for _, want := range []string{`value="RF52"`, `value="190"`, `value="VRCA220"`, `value="220"`, `<option value="passthrough" selected>passthrough</option>`, `<option value="alpha" selected>alpha packed</option>`, `<option value="perspective" selected>`} {
		if !strings.Contains(tmpl, want) {
			t.Errorf("preset template missing %q", want)
		}
	}
}

func TestRulePresets_AreValidRules(t *testing.T) {
	newEnv(t, &fakeStash{})
	presets := rulePresets()
	rules := make([]config.VideoRule, len(presets))
	for i := range presets {
		rules[i] = presets[i].Rule
	}
	cfg := config.Application()
	cfg.VideoRules = rules
	if err := config.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	if p := presets[4].Rule; !p.Passthrough || p.Background != "passthrough" || p.Mask != "alpha" || !p.GeneratesProfile() {
		t.Fatalf("passthrough preset %+v", p)
	}
}

func TestSetup_RendersSceneInspector(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	body := getPage(t, PagesRouter(lib), "/setup", nil).Body.String()
	for _, want := range []string{"Scene inspector", `id="inspect-q"`, `placeholder="Scene id or title"`, `id="inspect"`, `id="inspect-out"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Index(body, `id="inspect-q"`) < strings.Index(body, `id="save-rules"`) {
		t.Error("the inspector belongs under the rules")
	}
}
