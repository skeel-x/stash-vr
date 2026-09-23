package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Filter is a per-saved-filter override: display order, optional rename,
// whether the section is hidden from every player, and which players it
// is hidden from while staying on for the others.
type Filter struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Disabled bool     `json:"disabled"`
	HiddenIn []string `json:"hidden_in,omitempty"`
}

// Players lists the player names a Filter.HiddenIn entry may name.
var Players = []string{"heresphere", "deovr", "playa"}

func validateFilters(filters []Filter) error {
	for i, f := range filters {
		for _, v := range f.HiddenIn {
			if !slices.Contains(Players, v) {
				return fmt.Errorf("%w: filters[%d].hidden_in contains unknown player %q", ErrInvalid, i, v)
			}
		}
	}
	return nil
}

// VideoRule maps a Stash tag to player format settings. Rules apply in
// order; every non-empty field overrides what earlier rules set.
type VideoRule struct {
	Tag         string  `json:"tag"`
	Projection  string  `json:"projection,omitempty"`
	Stereo      string  `json:"stereo,omitempty"`
	Fov         float32 `json:"fov,omitempty"`
	Lens        string  `json:"lens,omitempty"`
	Passthrough bool    `json:"passthrough,omitempty"`
	Profile     string  `json:"profile,omitempty"`
}

var (
	validProjections = map[string]struct{}{"equirectangular": {}, "equirectangular360": {}, "fisheye": {}, "cubemap": {}, "equiangularCubemap": {}, "perspective": {}}
	validStereo      = map[string]struct{}{"mono": {}, "sbs": {}, "tb": {}}
	validLenses      = map[string]struct{}{"MKX200": {}, "MKX220": {}, "VRCA220": {}}
)

// DefaultVideoRules reproduces the mapping earlier releases had in code,
// plus passthrough for the tags that mark alpha-packed videos.
func DefaultVideoRules() []VideoRule {
	return []VideoRule{
		{Tag: "DOME", Projection: "equirectangular", Stereo: "sbs"},
		{Tag: "SPHERE", Projection: "equirectangular360", Stereo: "sbs"},
		{Tag: "FISHEYE", Projection: "fisheye", Stereo: "sbs"},
		{Tag: "MKX200", Projection: "fisheye", Stereo: "sbs", Lens: "MKX200", Fov: 200},
		{Tag: "RF52", Projection: "fisheye", Stereo: "sbs", Fov: 190},
		{Tag: "CUBEMAP", Projection: "cubemap", Stereo: "sbs"},
		{Tag: "EAC", Projection: "equiangularCubemap", Stereo: "sbs"},
		{Tag: "FLAT", Projection: "perspective", Stereo: "mono"},
		{Tag: "SBS", Stereo: "sbs"},
		{Tag: "TB", Stereo: "tb"},
		{Tag: "Passthrough", Passthrough: true},
		{Tag: "Alpha", Passthrough: true},
		{Tag: "Augmented Reality", Passthrough: true},
	}
}

func validateVideoRules(rules []VideoRule) error {
	for i, r := range rules {
		if strings.TrimSpace(r.Tag) == "" {
			return fmt.Errorf("%w: video_rules[%d].tag must not be empty", ErrInvalid, i)
		}
		if _, ok := validProjections[r.Projection]; r.Projection != "" && !ok {
			return fmt.Errorf("%w: video_rules[%d].projection %q is not supported", ErrInvalid, i, r.Projection)
		}
		if _, ok := validStereo[r.Stereo]; r.Stereo != "" && !ok {
			return fmt.Errorf("%w: video_rules[%d].stereo %q is not supported", ErrInvalid, i, r.Stereo)
		}
		if _, ok := validLenses[r.Lens]; r.Lens != "" && !ok {
			return fmt.Errorf("%w: video_rules[%d].lens %q is not supported", ErrInvalid, i, r.Lens)
		}
		if r.Fov != 0 && (r.Fov < 1 || r.Fov > 360) {
			return fmt.Errorf("%w: video_rules[%d].fov must be 0 or between 1 and 360", ErrInvalid, i)
		}
		for _, c := range r.Profile {
			if c < '0' || c > '9' {
				return fmt.Errorf("%w: video_rules[%d].profile must be a scene id", ErrInvalid, i)
			}
		}
	}
	return nil
}

// normalizeVideoRules trims tags in place.
func normalizeVideoRules(rules []VideoRule) {
	for i := range rules {
		rules[i].Tag = strings.TrimSpace(rules[i].Tag)
	}
}

const configFileName = "config.json"

// ErrInvalid marks a settings value rejected by Validate, so API callers can
// tell a bad request from a failed write.
var ErrInvalid = errors.New("invalid settings")

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
	StashGraphQLUrl    *string     `json:"stash_graphql_url,omitempty"`
	StashApiKey        *string     `json:"stash_api_key,omitempty"`
	FavoriteTag        *string     `json:"favorite_tag,omitempty"`
	ExcludeSortName    *string     `json:"exclude_sort_name,omitempty"`
	GenerateSummaryIds *bool       `json:"generate_summary_ids,omitempty"`
	HeatmapHeightPx    *int        `json:"heatmap_height_px,omitempty"`
	SmartSectionSize   *int        `json:"smart_section_size,omitempty"`
	ForceHTTPS         *bool       `json:"force_https,omitempty"`
	BasePath           *string     `json:"base_path,omitempty"`
	DeovrAutoload      *bool       `json:"deovr_autoload,omitempty"`
	FunscriptIndexPath *string     `json:"funscript_index_path,omitempty"`
	LogLevel           *string     `json:"log_level,omitempty"`
	Filters            []Filter    `json:"filters"`
	VideoRules         []VideoRule `json:"video_rules"`
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
	for i := range out.Filters {
		out.Filters[i].HiddenIn = slices.Clone(out.Filters[i].HiddenIn)
	}
	out.VideoRules = make([]VideoRule, len(c.VideoRules))
	copy(out.VideoRules, c.VideoRules)
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
	if seed.VideoRules == nil {
		seed.VideoRules = DefaultVideoRules()
	}
	if seed.BasePath, err = NormalizeBasePath(seed.BasePath); err != nil {
		return err
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
	if merged.BasePath, err = NormalizeBasePath(merged.BasePath); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
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
	if fc.SmartSectionSize != nil {
		base.SmartSectionSize = *fc.SmartSectionSize
	}
	if fc.ForceHTTPS != nil {
		base.ForceHTTPS = *fc.ForceHTTPS
	}
	if fc.BasePath != nil {
		base.BasePath = *fc.BasePath
	}
	if fc.DeovrAutoload != nil {
		base.DeovrAutoload = *fc.DeovrAutoload
	}
	if fc.FunscriptIndexPath != nil {
		base.FunscriptIndexPath = *fc.FunscriptIndexPath
	}
	if fc.LogLevel != nil {
		base.LogLevel = *fc.LogLevel
	}
	if fc.Filters != nil {
		base.Filters = fc.Filters
	}
	if fc.VideoRules != nil {
		base.VideoRules = fc.VideoRules
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
	if next.VideoRules == nil {
		next.VideoRules = []VideoRule{}
	}
	normalizeVideoRules(next.VideoRules)
	var err error
	if next.BasePath, err = NormalizeBasePath(next.BasePath); err != nil {
		return ApplicationConfig{}, err
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

// NormalizeBasePath returns "" or "/segment[/segment]" for p.
func NormalizeBasePath(p string) (string, error) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return "", nil
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: base_path must be like /stashvr, got %q", ErrInvalid, p)
		}
	}
	return "/" + p, nil
}

// Validate checks the runtime-changeable fields.
func Validate(c ApplicationConfig) error {
	u, err := url.Parse(c.StashGraphQLUrl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: stash_graphql_url must be an absolute http(s) URL, got %q", ErrInvalid, c.StashGraphQLUrl)
	}
	if _, ok := validLogLevels[c.LogLevel]; !ok {
		return fmt.Errorf("%w: log_level must be one of trace, debug, info, warn, error, got %q", ErrInvalid, c.LogLevel)
	}
	if c.HeatmapHeightPx < 0 || c.HeatmapHeightPx > 200 {
		return fmt.Errorf("%w: heatmap_height_px must be between 0 and 200, got %d", ErrInvalid, c.HeatmapHeightPx)
	}
	if c.SmartSectionSize < 10 || c.SmartSectionSize > 500 {
		return fmt.Errorf("%w: smart_section_size must be between 10 and 500, got %d", ErrInvalid, c.SmartSectionSize)
	}
	// An empty favorite tag is allowed: it disables favorite sync
	// (see library.Service.UpdateFavorite).
	if c.ExcludeSortName == "" {
		return fmt.Errorf("%w: exclude_sort_name must not be empty", ErrInvalid)
	}
	if c.FunscriptIndexPath != "" && !filepath.IsAbs(c.FunscriptIndexPath) {
		return fmt.Errorf("%w: funscript_index_path must be empty or an absolute path, got %q", ErrInvalid, c.FunscriptIndexPath)
	}
	if err := validateFilters(c.Filters); err != nil {
		return err
	}
	return validateVideoRules(c.VideoRules)
}

func write(path string, c ApplicationConfig) error {
	fc := fileConfig{
		StashGraphQLUrl:    &c.StashGraphQLUrl,
		StashApiKey:        &c.StashApiKey,
		FavoriteTag:        &c.FavoriteTag,
		ExcludeSortName:    &c.ExcludeSortName,
		GenerateSummaryIds: &c.GenerateSummaryIds,
		HeatmapHeightPx:    &c.HeatmapHeightPx,
		SmartSectionSize:   &c.SmartSectionSize,
		ForceHTTPS:         &c.ForceHTTPS,
		BasePath:           &c.BasePath,
		DeovrAutoload:      &c.DeovrAutoload,
		FunscriptIndexPath: &c.FunscriptIndexPath,
		LogLevel:           &c.LogLevel,
		Filters:            c.Filters,
		VideoRules:         c.VideoRules,
	}
	if fc.Filters == nil {
		fc.Filters = []Filter{}
	}
	if fc.VideoRules == nil {
		fc.VideoRules = []VideoRule{}
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
