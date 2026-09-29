package config

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
	"sort"
	"strings"
)

const (
	envKeyListenAddress      = "LISTEN_ADDRESS"
	envKeyStashGraphQLUrl    = "STASH_GRAPHQL_URL"
	envKeyStashApiKey        = "STASH_API_KEY"
	envKeyStashTLSInsecure   = "STASH_TLS_INSECURE"
	envKeyFavoriteTag        = "FAVORITE_TAG"
	envKeyLogLevel           = "LOG_LEVEL"
	envKeyDisableLogColor    = "DISABLE_LOG_COLOR"
	envKeyDisableRedact      = "DISABLE_REDACT"
	envKeyForceHTTPS         = "FORCE_HTTPS"
	envKeyBasePath           = "BASE_PATH"
	envKeyDeovrAutoload      = "DEOVR_AUTOLOAD"
	envKeyPerformerFacets    = "PERFORMER_FACETS"
	envKeyDateLookup         = "DATE_LOOKUP"
	envKeyDateWriteback      = "DATE_WRITEBACK"
	envKeyFunscriptIndexPath = "FUNSCRIPT_INDEX_PATH"
	envKeyHeatmapHeightPx    = "HEATMAP_HEIGHT_PX"
	envKeyExcludeSortName    = "EXCLUDE_SORT_NAME"
	envKeyUserConfigPath     = "CONFIG_PATH"
	envKeyGenerateSummaryIds = "GENERATE_SUMMARY_IDS"
	envKeySmartSectionSize   = "SMART_SECTION_SIZE"
	envKeyAutoStudioMin      = "AUTO_STUDIO_MIN"
	envKeyAutoPerformerMin   = "AUTO_PERFORMER_MIN"
	envKeyCoverBadgeQuality  = "COVER_BADGE_QUALITY"
	envKeyCoverBadgeFormat   = "COVER_BADGE_FORMAT"
	envKeyCoverBadgePass     = "COVER_BADGE_PASSTHROUGH"
	envKeyCoverBadgeDuration = "COVER_BADGE_DURATION"
	envKeyCoverBadgeRate     = "COVER_BADGE_FRAMERATE"
	envKeyLearnStudio        = "LEARN_STUDIO_PROFILES"
	envKeyCorrectVertical    = "CORRECT_VERTICAL_STEREO"
)

type ApplicationConfig struct {
	ListenAddress   string
	StashGraphQLUrl string
	StashApiKey     string
	// StashTLSInsecure skips verifying the certificate of an https Stash,
	// for self-signed certificates. Verification is on by default (see
	// internal/stash/transport.go for the behaviour change).
	StashTLSInsecure   bool
	FavoriteTag        string
	LogLevel           string
	DisableLogColor    bool
	IsRedactDisabled   bool
	ForceHTTPS         bool
	BasePath           string
	DeovrAutoload      bool
	PerformerFacets    bool
	DateLookup         bool
	DateWriteback      bool
	FunscriptIndexPath string
	HeatmapHeightPx    int
	SmartSectionSize   int
	AutoStudioMin      int
	AutoPerformerMin   int
	ExcludeSortName    string
	ConfigPath         string
	GenerateSummaryIds bool
	CoverBadges        CoverBadges
	// LearnStudioProfiles gives a scene without a HereSphere profile of
	// its own the newest one saved for a scene of the same studio and lens.
	LearnStudioProfiles bool
	// CorrectVerticalStereo pitches one eye in generated HereSphere
	// profiles by the vertical offset vrQualityTags measured for a scene.
	CorrectVerticalStereo bool
	Filters               []Filter
	VideoRules            []VideoRule
}

// registerFlags declares every setting flag on fs and binds each one with
// bind (viper.BindPFlag in production), so the declarations can be checked
// without parsing the process arguments.
func registerFlags(fs *pflag.FlagSet, bind func(key string, flag *pflag.Flag) error) {
	fs.String(envKeyListenAddress, ":9666", "Local address for Stash-VR to listen on")
	_ = bind(envKeyListenAddress, fs.Lookup(envKeyListenAddress))

	fs.String(envKeyStashGraphQLUrl, "http://localhost:9999/graphql", "Url to Stash graphql")
	_ = bind(envKeyStashGraphQLUrl, fs.Lookup(envKeyStashGraphQLUrl))

	fs.String(envKeyStashApiKey, "", "Stash API key")
	_ = bind(envKeyStashApiKey, fs.Lookup(envKeyStashApiKey))

	fs.Bool(envKeyStashTLSInsecure, false, "Skip verifying the TLS certificate of an https Stash (self-signed certificates)")
	_ = bind(envKeyStashTLSInsecure, fs.Lookup(envKeyStashTLSInsecure))

	fs.String(envKeyFavoriteTag, "FAVORITE", "Name of tag in Stash to hold scenes marked as favorites")
	_ = bind(envKeyFavoriteTag, fs.Lookup(envKeyFavoriteTag))

	fs.String(envKeyLogLevel, "info", "Set log level - trace, debug, warn, info or error")
	_ = bind(envKeyLogLevel, fs.Lookup(envKeyLogLevel))

	fs.Bool(envKeyDisableLogColor, false, "Disable colors in log output")
	_ = bind(envKeyDisableLogColor, fs.Lookup(envKeyDisableLogColor))

	fs.Bool(envKeyDisableRedact, false, "Disable redacting sensitive information from logs")
	_ = bind(envKeyDisableRedact, fs.Lookup(envKeyDisableRedact))

	fs.Bool(envKeyForceHTTPS, false, "Force Stash-VR to use HTTPS")
	_ = bind(envKeyForceHTTPS, fs.Lookup(envKeyForceHTTPS))

	fs.String(envKeyBasePath, "", "Path prefix when served under a sub-path behind a reverse proxy, e.g. /stashvr")
	_ = bind(envKeyBasePath, fs.Lookup(envKeyBasePath))

	fs.Bool(envKeyDeovrAutoload, true, "Send the DeoVR library when DeoVR's browser opens the front page")
	_ = bind(envKeyDeovrAutoload, fs.Lookup(envKeyDeovrAutoload))

	fs.Bool(envKeyPerformerFacets, true, "Add Country and Age tags derived from a scene's performers in HereSphere")
	_ = bind(envKeyPerformerFacets, fs.Lookup(envKeyPerformerFacets))

	fs.Bool(envKeyDateLookup, true, "Look up missing release dates from the stash-boxes configured in Stash")
	_ = bind(envKeyDateLookup, fs.Lookup(envKeyDateLookup))

	fs.Bool(envKeyDateWriteback, false, "Write release dates found on stash-boxes back to Stash")
	_ = bind(envKeyDateWriteback, fs.Lookup(envKeyDateWriteback))

	fs.String(envKeyFunscriptIndexPath, "", "Path to the timestampTrade plugin's funscript_index.sqlite; its scripts are offered as alternates")
	_ = bind(envKeyFunscriptIndexPath, fs.Lookup(envKeyFunscriptIndexPath))

	fs.Int(envKeyHeatmapHeightPx, 0, "Height of heatmaps")
	_ = bind(envKeyHeatmapHeightPx, fs.Lookup(envKeyHeatmapHeightPx))

	fs.Int(envKeySmartSectionSize, 50, "Number of scenes in each smart section")
	_ = bind(envKeySmartSectionSize, fs.Lookup(envKeySmartSectionSize))

	fs.Int(envKeyAutoStudioMin, 0, "Generate a section for every studio with at least this many scenes (0 turns it off)")
	_ = bind(envKeyAutoStudioMin, fs.Lookup(envKeyAutoStudioMin))

	fs.Int(envKeyAutoPerformerMin, 0, "Generate a section for every performer with at least this many scenes (0 turns it off)")
	_ = bind(envKeyAutoPerformerMin, fs.Lookup(envKeyAutoPerformerMin))

	fs.String(envKeyExcludeSortName, "hidden", "Exclude tags with this sort name")
	_ = bind(envKeyExcludeSortName, fs.Lookup(envKeyExcludeSortName))

	fs.String(envKeyUserConfigPath, "", "Path to store user config (may contain filter names in plain text)")
	_ = bind(envKeyUserConfigPath, fs.Lookup(envKeyUserConfigPath))

	fs.Bool(envKeyGenerateSummaryIds, false, "Generate summary ids for categorized tags")
	_ = bind(envKeyGenerateSummaryIds, fs.Lookup(envKeyGenerateSummaryIds))

	fs.Bool(envKeyCoverBadgeQuality, true, "Draw the quality tier or resolution onto scene covers")
	_ = bind(envKeyCoverBadgeQuality, fs.Lookup(envKeyCoverBadgeQuality))

	fs.Bool(envKeyCoverBadgeFormat, false, "Draw the projection (180, 360, FISHEYE, FLAT 3D) onto scene covers")
	_ = bind(envKeyCoverBadgeFormat, fs.Lookup(envKeyCoverBadgeFormat))

	fs.Bool(envKeyCoverBadgePass, true, "Draw an AR badge onto covers of passthrough scenes")
	_ = bind(envKeyCoverBadgePass, fs.Lookup(envKeyCoverBadgePass))

	fs.Bool(envKeyCoverBadgeDuration, true, "Draw the running time onto scene covers")
	_ = bind(envKeyCoverBadgeDuration, fs.Lookup(envKeyCoverBadgeDuration))

	fs.Bool(envKeyCoverBadgeRate, true, "Draw the frame rate onto scene covers")
	_ = bind(envKeyCoverBadgeRate, fs.Lookup(envKeyCoverBadgeRate))

	fs.Bool(envKeyLearnStudio, false, "Use a saved HereSphere profile for other scenes from the same studio with the same lens")
	_ = bind(envKeyLearnStudio, fs.Lookup(envKeyLearnStudio))

	fs.Bool(envKeyCorrectVertical, true, "Correct the vertical misalignment vrQualityTags measured between the eyes in generated HereSphere profiles")
	_ = bind(envKeyCorrectVertical, fs.Lookup(envKeyCorrectVertical))

	fs.BoolP("help", "h", false, "Display usage information")
	_ = bind("help", fs.Lookup("help"))
}

func Init() error {
	registerFlags(pflag.CommandLine, viper.BindPFlag)

	pflag.Parse()

	if viper.GetBool("help") {
		pflag.Usage()
		os.Exit(1)
	}

	viper.AutomaticEnv()

	seed := ApplicationConfig{
		ListenAddress:      viper.GetString(envKeyListenAddress),
		StashGraphQLUrl:    viper.GetString(envKeyStashGraphQLUrl),
		StashApiKey:        viper.GetString(envKeyStashApiKey),
		StashTLSInsecure:   viper.GetBool(envKeyStashTLSInsecure),
		FavoriteTag:        viper.GetString(envKeyFavoriteTag),
		LogLevel:           strings.ToLower(viper.GetString(envKeyLogLevel)),
		DisableLogColor:    viper.GetBool(envKeyDisableLogColor),
		IsRedactDisabled:   viper.GetBool(envKeyDisableRedact),
		ForceHTTPS:         viper.GetBool(envKeyForceHTTPS),
		BasePath:           viper.GetString(envKeyBasePath),
		DeovrAutoload:      viper.GetBool(envKeyDeovrAutoload),
		PerformerFacets:    viper.GetBool(envKeyPerformerFacets),
		DateLookup:         viper.GetBool(envKeyDateLookup),
		DateWriteback:      viper.GetBool(envKeyDateWriteback),
		FunscriptIndexPath: viper.GetString(envKeyFunscriptIndexPath),
		HeatmapHeightPx:    viper.GetInt(envKeyHeatmapHeightPx),
		SmartSectionSize:   viper.GetInt(envKeySmartSectionSize),
		AutoStudioMin:      viper.GetInt(envKeyAutoStudioMin),
		AutoPerformerMin:   viper.GetInt(envKeyAutoPerformerMin),
		ExcludeSortName:    viper.GetString(envKeyExcludeSortName),
		ConfigPath:         viper.GetString(envKeyUserConfigPath),
		GenerateSummaryIds: viper.GetBool(envKeyGenerateSummaryIds),
		CoverBadges: CoverBadges{
			Quality:     viper.GetBool(envKeyCoverBadgeQuality),
			Format:      viper.GetBool(envKeyCoverBadgeFormat),
			Passthrough: viper.GetBool(envKeyCoverBadgePass),
			Duration:    viper.GetBool(envKeyCoverBadgeDuration),
			FrameRate:   viper.GetBool(envKeyCoverBadgeRate),
		},
		LearnStudioProfiles:   viper.GetBool(envKeyLearnStudio),
		CorrectVerticalStereo: viper.GetBool(envKeyCorrectVertical),
	}

	err := Load(seed)
	loaded := Application()
	envOverrides = ignoredOverrides(viper.IsSet, &seed, &loaded)
	return err
}

// envOverrides is what EnvOverrides returns; set once by Init.
var envOverrides []string

// EnvOverrides lists the settings given as a flag or environment variable
// at startup whose value differs from config.json, which wins: the
// variable is being ignored, which is worth a warning once the logger is
// up. Keys only; the values may be secrets and are never reported.
func EnvOverrides() []string { return envOverrides }

// settingValues maps each persisted setting's flag/env key to its value in
// c, so a seed can be compared with what was loaded.
func settingValues(c *ApplicationConfig) map[string]any {
	return map[string]any{
		envKeyStashGraphQLUrl:    c.StashGraphQLUrl,
		envKeyStashApiKey:        c.StashApiKey,
		envKeyStashTLSInsecure:   c.StashTLSInsecure,
		envKeyFavoriteTag:        c.FavoriteTag,
		envKeyLogLevel:           c.LogLevel,
		envKeyForceHTTPS:         c.ForceHTTPS,
		envKeyBasePath:           c.BasePath,
		envKeyDeovrAutoload:      c.DeovrAutoload,
		envKeyPerformerFacets:    c.PerformerFacets,
		envKeyDateLookup:         c.DateLookup,
		envKeyDateWriteback:      c.DateWriteback,
		envKeyFunscriptIndexPath: c.FunscriptIndexPath,
		envKeyHeatmapHeightPx:    c.HeatmapHeightPx,
		envKeyExcludeSortName:    c.ExcludeSortName,
		envKeyGenerateSummaryIds: c.GenerateSummaryIds,
		envKeySmartSectionSize:   c.SmartSectionSize,
		envKeyAutoStudioMin:      c.AutoStudioMin,
		envKeyAutoPerformerMin:   c.AutoPerformerMin,
		envKeyCoverBadgeQuality:  c.CoverBadges.Quality,
		envKeyCoverBadgeFormat:   c.CoverBadges.Format,
		envKeyCoverBadgePass:     c.CoverBadges.Passthrough,
		envKeyCoverBadgeDuration: c.CoverBadges.Duration,
		envKeyCoverBadgeRate:     c.CoverBadges.FrameRate,
		envKeyLearnStudio:        c.LearnStudioProfiles,
		envKeyCorrectVertical:    c.CorrectVerticalStereo,
	}
}

// ignoredOverrides returns, sorted, the keys isSet reports as given whose
// value in seed differs from loaded. The seed is compared as Load would
// store it, so a value that only differs in form is not reported.
func ignoredOverrides(isSet func(string) bool, seed, loaded *ApplicationConfig) []string {
	normalized := *seed
	normalize(&normalized)
	if p, err := NormalizeBasePath(normalized.BasePath); err == nil {
		normalized.BasePath = p
	}
	sv, lv := settingValues(&normalized), settingValues(loaded)
	var out []string
	for k := range sv {
		if isSet(k) && sv[k] != lv[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (a ApplicationConfig) Redacted() ApplicationConfig {
	a.StashGraphQLUrl = Redacted(a.StashGraphQLUrl)
	a.StashApiKey = Redacted(a.StashApiKey)
	a.ConfigPath = Redacted(a.ConfigPath)
	return a
}
