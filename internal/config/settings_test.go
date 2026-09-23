package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedFor(t *testing.T) ApplicationConfig {
	t.Helper()
	return ApplicationConfig{
		ListenAddress:    ":9666",
		StashGraphQLUrl:  "http://stash:9999/graphql",
		StashApiKey:      "secret",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		ConfigPath:       t.TempDir(),
	}
}

func TestLoad_WritesSeedWhenFileMissing(t *testing.T) {
	seed := seedFor(t)

	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := os.Stat(FilePath(seed)); err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if got := Application().StashGraphQLUrl; got != seed.StashGraphQLUrl {
		t.Fatalf("expected seed url, got %q", got)
	}
	if got := Application().Filters; got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil filters, got %#v", got)
	}
}

func TestLoad_FileWinsOverSeedAndMissingKeysFallBack(t *testing.T) {
	seed := seedFor(t)
	content := `{"stash_graphql_url":"http://other:9999/graphql","log_level":"debug","filters":[{"id":"7","name":"","disabled":true}]}`
	if err := os.WriteFile(FilePath(seed), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg := Application()
	if cfg.StashGraphQLUrl != "http://other:9999/graphql" {
		t.Fatalf("expected file url to win, got %q", cfg.StashGraphQLUrl)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected file log level to win, got %q", cfg.LogLevel)
	}
	if cfg.FavoriteTag != "FAVORITE" {
		t.Fatalf("expected seed favorite tag for a key missing from the file, got %q", cfg.FavoriteTag)
	}
	if cfg.StashApiKey != "secret" {
		t.Fatalf("expected seed api key for a key missing from the file, got %q", cfg.StashApiKey)
	}
	if len(cfg.Filters) != 1 || cfg.Filters[0].ID != "7" || !cfg.Filters[0].Disabled {
		t.Fatalf("expected filters from file, got %#v", cfg.Filters)
	}
}

func TestLoad_PerformerFacetsSeedAndFile(t *testing.T) {
	seed := seedFor(t)
	seed.PerformerFacets = true

	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !Application().PerformerFacets {
		t.Fatal("expected performer facets on from the seed")
	}
	data, err := os.ReadFile(FilePath(seed))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"performer_facets": true`) {
		t.Fatalf("expected performer_facets persisted, got %s", data)
	}

	if err := os.WriteFile(FilePath(seed), []byte(`{"performer_facets":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Application().PerformerFacets {
		t.Fatal("expected the file to turn performer facets off")
	}
}

func TestLoad_UnparsableFileFailsNamingPath(t *testing.T) {
	seed := seedFor(t)
	if err := os.WriteFile(FilePath(seed), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Load(seed)

	if err == nil || !strings.Contains(err.Error(), FilePath(seed)) {
		t.Fatalf("expected error naming %s, got %v", FilePath(seed), err)
	}
}

func TestSet_PersistsRuntimeFieldsAndKeepsProcessFields(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}

	cfg := Application()
	cfg.LogLevel = "warn"
	cfg.HeatmapHeightPx = 20
	cfg.ListenAddress = ":1" // process field, must be ignored
	if _, err := Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got := Application(); got.LogLevel != "warn" || got.HeatmapHeightPx != 20 {
		t.Fatalf("expected new values in store, got %#v", got)
	}
	if got := Application().ListenAddress; got != ":9666" {
		t.Fatalf("expected listen address unchanged, got %q", got)
	}

	// A fresh Load from the same seed must see the persisted values.
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	if got := Application(); got.LogLevel != "warn" || got.HeatmapHeightPx != 20 {
		t.Fatalf("expected persisted values after reload, got %#v", got)
	}
	data, _ := os.ReadFile(filepath.Join(seed.ConfigPath, "config.json"))
	if strings.Contains(string(data), "listen_address") {
		t.Fatal("process fields must not be persisted")
	}
}

func TestSet_RejectsInvalidAndLeavesStoreUnchanged(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*ApplicationConfig){
		"relative url":     func(c *ApplicationConfig) { c.StashGraphQLUrl = "graphql" },
		"bad log level":    func(c *ApplicationConfig) { c.LogLevel = "loud" },
		"height too large": func(c *ApplicationConfig) { c.HeatmapHeightPx = 201 },
		"empty exclude":    func(c *ApplicationConfig) { c.ExcludeSortName = "" },
	}
	for name, mutate := range cases {
		cfg := Application()
		mutate(&cfg)
		if _, err := Set(cfg); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	if got := Application(); got.StashGraphQLUrl != seed.StashGraphQLUrl || got.LogLevel != "info" {
		t.Fatalf("store must be unchanged after rejected Set, got %#v", got)
	}
}

func TestApplication_ReturnedFiltersDoNotAliasNextSet(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	cfg := Application()
	cfg.Filters = append(cfg.Filters, Filter{ID: "x"})
	if got := Application().Filters; len(got) != 0 {
		t.Fatalf("appending to a returned config must not change the store, got %v", got)
	}
}

func TestWrite_LeavesNoTempFileAndIsAtomic(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(seed.ConfigPath)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if _, err := os.Stat(FilePath(seed)); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_UnwritableDirReturnsSeedNotPersisted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	seed := seedFor(t)
	seed.ConfigPath = filepath.Join(seed.ConfigPath, "ro")
	if err := os.MkdirAll(seed.ConfigPath, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(seed.ConfigPath, 0o700) })

	err := Load(seed)

	if !errors.Is(err, ErrSeedNotPersisted) {
		t.Fatalf("expected ErrSeedNotPersisted, got %v", err)
	}
	if Application().StashGraphQLUrl != seed.StashGraphQLUrl {
		t.Fatal("seed must still be applied in memory")
	}
}

func TestSet_AllowsEmptyFavoriteTag(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}

	cfg := Application()
	cfg.FavoriteTag = ""
	if _, err := Set(cfg); err != nil {
		t.Fatalf("an empty favorite tag disables favorite sync and must be accepted: %v", err)
	}
	if got := Application().FavoriteTag; got != "" {
		t.Fatalf("expected the empty favorite tag to be stored, got %q", got)
	}
}

func TestSet_SmartSectionSizeBounds(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []int{9, 501} {
		cfg := Application()
		cfg.SmartSectionSize = bad
		if _, err := Set(cfg); err == nil {
			t.Errorf("expected smart_section_size %d to be rejected", bad)
		}
	}
	cfg := Application()
	cfg.SmartSectionSize = 120
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	if got := Application().SmartSectionSize; got != 120 {
		t.Fatalf("expected persisted 120, got %d", got)
	}
}

func TestSet_BasePathNormalisedAndValidated(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	cfg := Application()
	cfg.BasePath = "stashvr/"
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := Application().BasePath; got != "/stashvr" {
		t.Fatalf("expected normalised /stashvr, got %q", got)
	}
	cfg.BasePath = "/a//b"
	if _, err := Set(cfg); err == nil {
		t.Fatal("expected a path with empty segments to be rejected")
	}
	cfg.BasePath = ""
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSet_FunscriptIndexPathMustBeAbsolute(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	cfg := Application()
	cfg.FunscriptIndexPath = "relative/index.sqlite"
	if _, err := Set(cfg); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a relative path, got %v", err)
	}
	cfg.FunscriptIndexPath = "/opt/stash/funscript_index.sqlite"
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := Application().FunscriptIndexPath; got != "/opt/stash/funscript_index.sqlite" {
		t.Fatalf("stored %q", got)
	}
	cfg.FunscriptIndexPath = ""
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_SeedsDefaultVideoRulesWhenAbsent(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	rules := Application().VideoRules
	if len(rules) != 13 || rules[0].Tag != "DOME" || rules[0].Projection != "equirectangular" || rules[12].Tag != "Augmented Reality" || !rules[12].Passthrough {
		t.Fatalf("expected the 13 default rules, got %+v", rules)
	}
	data, _ := os.ReadFile(FilePath(Application()))
	if !strings.Contains(string(data), `"video_rules"`) {
		t.Fatalf("expected video_rules persisted, got %s", data)
	}
}

func TestLoad_KeepsEmptyVideoRulesFromFile(t *testing.T) {
	seed := seedFor(t)
	path := filepath.Join(seed.ConfigPath, "config.json")
	if err := os.WriteFile(path, []byte(`{"stash_graphql_url":"http://stash:9999/graphql","video_rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	if got := Application().VideoRules; len(got) != 0 {
		t.Fatalf("expected the file's empty rules to win, got %+v", got)
	}
}

func TestSet_ValidatesVideoRules(t *testing.T) {
	if err := Load(seedFor(t)); err != nil {
		t.Fatal(err)
	}
	bad := []VideoRule{
		{Tag: "X", Projection: "dome"},
		{Tag: "X", Fov: 400},
		{Tag: "X", Profile: "abc"},
		{Tag: "  ", Stereo: "sbs"},
		{Tag: "X", Stereo: "cuv"},
		{Tag: "X", Lens: "GoPro"},
	}
	for _, r := range bad {
		cfg := Application()
		cfg.VideoRules = []VideoRule{r}
		if _, err := Set(cfg); !errors.Is(err, ErrInvalid) {
			t.Errorf("rule %+v: expected ErrInvalid, got %v", r, err)
		}
	}
	cfg := Application()
	cfg.VideoRules = []VideoRule{{Tag: " Passthrough ", Passthrough: true, Profile: "11649", Fov: 200, Lens: "MKX200"}}
	saved, err := Set(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if saved.VideoRules[0].Tag != "Passthrough" {
		t.Fatalf("expected the tag trimmed, got %q", saved.VideoRules[0].Tag)
	}
}

func TestSet_ValidatesHiddenIn(t *testing.T) {
	if err := Load(seedFor(t)); err != nil {
		t.Fatal(err)
	}
	cfg := Application()
	cfg.Filters = []Filter{{ID: "42", HiddenIn: []string{"playa", "vlc"}}}
	if _, err := Set(cfg); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for an unknown player, got %v", err)
	}
	cfg.Filters = []Filter{{ID: "42", HiddenIn: []string{"playa"}}}
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(FilePath(Application()))
	if !strings.Contains(string(data), `"hidden_in"`) {
		t.Fatalf("expected hidden_in persisted, got %s", data)
	}
}

func TestLoad_DateSettingsSeedAndFile(t *testing.T) {
	seed := seedFor(t)
	seed.DateLookup = true

	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !Application().DateLookup || Application().DateWriteback {
		t.Fatal("expected lookup on and write-back off from the seed")
	}
	data, err := os.ReadFile(FilePath(seed))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"date_lookup": true`) || !strings.Contains(string(data), `"date_writeback": false`) {
		t.Fatalf("expected both date settings persisted, got %s", data)
	}

	if err := os.WriteFile(FilePath(seed), []byte(`{"date_lookup":false,"date_writeback":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Application().DateLookup || !Application().DateWriteback {
		t.Fatal("expected the file to turn lookup off and write-back on")
	}
}

func TestSet_AutoSectionMinBounds(t *testing.T) {
	seed := seedFor(t)
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []int{-1, 10001} {
		cfg := Application()
		cfg.AutoStudioMin = bad
		if _, err := Set(cfg); !errors.Is(err, ErrInvalid) {
			t.Errorf("expected auto_studio_min %d to be rejected as invalid, got %v", bad, err)
		}
		cfg = Application()
		cfg.AutoPerformerMin = bad
		if _, err := Set(cfg); !errors.Is(err, ErrInvalid) {
			t.Errorf("expected auto_performer_min %d to be rejected as invalid, got %v", bad, err)
		}
	}
	cfg := Application()
	cfg.AutoStudioMin = 20
	cfg.AutoPerformerMin = 10000
	if _, err := Set(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); err != nil {
		t.Fatal(err)
	}
	if got := Application(); got.AutoStudioMin != 20 || got.AutoPerformerMin != 10000 {
		t.Fatalf("expected persisted 20 and 10000, got %d and %d", got.AutoStudioMin, got.AutoPerformerMin)
	}
}

func TestLoad_AutoSectionMinsSeedAndFile(t *testing.T) {
	seed := seedFor(t)
	seed.AutoStudioMin = 5

	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := os.ReadFile(FilePath(seed))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"auto_studio_min": 5`) || !strings.Contains(string(data), `"auto_performer_min": 0`) {
		t.Fatalf("expected both thresholds persisted, got %s", data)
	}

	if err := os.WriteFile(FilePath(seed), []byte(`{"auto_studio_min":0,"auto_performer_min":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := Application(); got.AutoStudioMin != 0 || got.AutoPerformerMin != 7 {
		t.Fatalf("expected the file values 0 and 7, got %d and %d", got.AutoStudioMin, got.AutoPerformerMin)
	}

	if err := os.WriteFile(FilePath(seed), []byte(`{"auto_performer_min":-3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(seed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected a negative threshold in the file to be rejected, got %v", err)
	}
}
