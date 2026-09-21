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
		ListenAddress:   ":9666",
		StashGraphQLUrl: "http://stash:9999/graphql",
		StashApiKey:     "secret",
		FavoriteTag:     "FAVORITE",
		LogLevel:        "info",
		ExcludeSortName: "hidden",
		ConfigPath:      t.TempDir(),
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
		"empty favorite":   func(c *ApplicationConfig) { c.FavoriteTag = "" },
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
