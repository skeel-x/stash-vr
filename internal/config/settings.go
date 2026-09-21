package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Filter is a per-saved-filter override: display order, optional rename,
// and whether the section is hidden from players.
type Filter struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
}

const configFileName = "config.json"

// ErrSeedNotPersisted reports that the seed was applied in memory but could
// not be written to disk, typically because the config dir is not writable.
// Callers may continue with the in-memory settings.
var ErrSeedNotPersisted = errors.New("settings could not be persisted")

var validLogLevels = map[string]struct{}{
	"trace": {}, "debug": {}, "info": {}, "warn": {}, "error": {},
}

// fileConfig is the persisted subset of ApplicationConfig. Pointer fields let
// a file that lacks a key fall back to the seed (flags/env/defaults).
type fileConfig struct {
	StashGraphQLUrl    *string  `json:"stash_graphql_url,omitempty"`
	StashApiKey        *string  `json:"stash_api_key,omitempty"`
	FavoriteTag        *string  `json:"favorite_tag,omitempty"`
	ExcludeSortName    *string  `json:"exclude_sort_name,omitempty"`
	GenerateSummaryIds *bool    `json:"generate_summary_ids,omitempty"`
	HeatmapHeightPx    *int     `json:"heatmap_height_px,omitempty"`
	ForceHTTPS         *bool    `json:"force_https,omitempty"`
	LogLevel           *string  `json:"log_level,omitempty"`
	Filters            []Filter `json:"filters"`
}

var (
	// mu serialises writers only; readers go through the atomic pointer.
	mu      sync.Mutex
	current atomic.Pointer[ApplicationConfig]
)

// Application returns the current settings. It neither locks nor allocates,
// so it is cheap enough for the per-tag player hot paths.
//
// The returned Filters slice is shared and must be treated as read-only;
// writers always store a fresh slice.
func Application() ApplicationConfig {
	if p := current.Load(); p != nil {
		return *p
	}
	return ApplicationConfig{}
}

// store publishes c as the current settings. The caller must hold mu and must
// pass a value whose Filters slice is not shared with anyone else.
func store(c ApplicationConfig) {
	current.Store(&c)
}

func cloneConfig(c ApplicationConfig) ApplicationConfig {
	out := c
	out.Filters = make([]Filter, len(c.Filters))
	copy(out.Filters, c.Filters)
	return out
}

// FilePath is where the settings in c are persisted.
func FilePath(c ApplicationConfig) string {
	return filepath.Join(c.ConfigPath, configFileName)
}

func resolveConfigDir(configPath string) (string, error) {
	if configPath != "" {
		return configPath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "config"), nil
}

// Load initialises the store from seed (flags/env/defaults) and the config
// file. Values in the file win; keys missing from the file keep the seed
// value. A missing file is written from the seed so the next start is
// file-driven. A file that cannot be parsed is an error naming the path.
func Load(seed ApplicationConfig) error {
	dir, err := resolveConfigDir(seed.ConfigPath)
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	seed.ConfigPath = dir
	if seed.Filters == nil {
		seed.Filters = []Filter{}
	}
	path := FilePath(seed)

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := Validate(seed); err != nil {
			return err
		}
		mu.Lock()
		store(cloneConfig(seed))
		mu.Unlock()
		if err := write(path, seed); err != nil {
			return fmt.Errorf("%w: %w", ErrSeedNotPersisted, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("read config %s: %w", path, err)
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	merged := applyFile(seed, fc)
	if err := Validate(merged); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	mu.Lock()
	store(cloneConfig(merged))
	mu.Unlock()
	return nil
}

func applyFile(base ApplicationConfig, fc fileConfig) ApplicationConfig {
	if fc.StashGraphQLUrl != nil {
		base.StashGraphQLUrl = *fc.StashGraphQLUrl
	}
	if fc.StashApiKey != nil {
		base.StashApiKey = *fc.StashApiKey
	}
	if fc.FavoriteTag != nil {
		base.FavoriteTag = *fc.FavoriteTag
	}
	if fc.ExcludeSortName != nil {
		base.ExcludeSortName = *fc.ExcludeSortName
	}
	if fc.GenerateSummaryIds != nil {
		base.GenerateSummaryIds = *fc.GenerateSummaryIds
	}
	if fc.HeatmapHeightPx != nil {
		base.HeatmapHeightPx = *fc.HeatmapHeightPx
	}
	if fc.ForceHTTPS != nil {
		base.ForceHTTPS = *fc.ForceHTTPS
	}
	if fc.LogLevel != nil {
		base.LogLevel = *fc.LogLevel
	}
	if fc.Filters != nil {
		base.Filters = fc.Filters
	}
	return base
}

// Set validates and stores the runtime-changeable fields of cfg, keeps the
// process-level fields from the current settings, and persists the result.
func Set(cfg ApplicationConfig) (ApplicationConfig, error) {
	mu.Lock()
	defer mu.Unlock()

	cur := Application()
	next := cloneConfig(cfg)
	next.ListenAddress = cur.ListenAddress
	next.DisableLogColor = cur.DisableLogColor
	next.IsRedactDisabled = cur.IsRedactDisabled
	next.ConfigPath = cur.ConfigPath
	if next.Filters == nil {
		next.Filters = []Filter{}
	}
	if err := Validate(next); err != nil {
		return ApplicationConfig{}, err
	}
	if err := write(FilePath(next), next); err != nil {
		return ApplicationConfig{}, err
	}
	store(next)
	return cloneConfig(next), nil
}

// Validate checks the runtime-changeable fields.
func Validate(c ApplicationConfig) error {
	u, err := url.Parse(c.StashGraphQLUrl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("stash_graphql_url must be an absolute http(s) URL, got %q", c.StashGraphQLUrl)
	}
	if _, ok := validLogLevels[c.LogLevel]; !ok {
		return fmt.Errorf("log_level must be one of trace, debug, info, warn, error, got %q", c.LogLevel)
	}
	if c.HeatmapHeightPx < 0 || c.HeatmapHeightPx > 200 {
		return fmt.Errorf("heatmap_height_px must be between 0 and 200, got %d", c.HeatmapHeightPx)
	}
	if c.FavoriteTag == "" {
		return errors.New("favorite_tag must not be empty")
	}
	if c.ExcludeSortName == "" {
		return errors.New("exclude_sort_name must not be empty")
	}
	return nil
}

func write(path string, c ApplicationConfig) error {
	fc := fileConfig{
		StashGraphQLUrl:    &c.StashGraphQLUrl,
		StashApiKey:        &c.StashApiKey,
		FavoriteTag:        &c.FavoriteTag,
		ExcludeSortName:    &c.ExcludeSortName,
		GenerateSummaryIds: &c.GenerateSummaryIds,
		HeatmapHeightPx:    &c.HeatmapHeightPx,
		ForceHTTPS:         &c.ForceHTTPS,
		LogLevel:           &c.LogLevel,
		Filters:            c.Filters,
	}
	if fc.Filters == nil {
		fc.Filters = []Filter{}
	}
	data, err := json.MarshalIndent(fc, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	// Write to a temp file in the same directory and rename over the target,
	// so a crash mid-write cannot leave a truncated config.json behind.
	tmp := path + ".tmp"
	if err := writeTemp(tmp, data); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

func writeTemp(tmp string, data []byte) error {
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
