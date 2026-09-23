package config

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
	"strings"
)

const (
	envKeyListenAddress      = "LISTEN_ADDRESS"
	envKeyStashGraphQLUrl    = "STASH_GRAPHQL_URL"
	envKeyStashApiKey        = "STASH_API_KEY"
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
)

type ApplicationConfig struct {
	ListenAddress      string
	StashGraphQLUrl    string
	StashApiKey        string
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
	Filters            []Filter
	VideoRules         []VideoRule
}

func Init() error {
	pflag.String(envKeyListenAddress, ":9666", "Local address for Stash-VR to listen on")
	_ = viper.BindPFlag(envKeyListenAddress, pflag.Lookup(envKeyListenAddress))

	pflag.String(envKeyStashGraphQLUrl, "http://localhost:9999/graphql", "Url to Stash graphql")
	_ = viper.BindPFlag(envKeyStashGraphQLUrl, pflag.Lookup(envKeyStashGraphQLUrl))

	pflag.String(envKeyStashApiKey, "", "Stash API key")
	_ = viper.BindPFlag(envKeyStashApiKey, pflag.Lookup(envKeyStashApiKey))

	pflag.String(envKeyFavoriteTag, "FAVORITE", "Name of tag in Stash to hold scenes marked as favorites")
	_ = viper.BindPFlag(envKeyFavoriteTag, pflag.Lookup(envKeyFavoriteTag))

	pflag.String(envKeyLogLevel, "info", "Set log level - trace, debug, warn, info or error")
	_ = viper.BindPFlag(envKeyLogLevel, pflag.Lookup(envKeyLogLevel))

	pflag.Bool(envKeyDisableLogColor, false, "Disable colors in log output")
	_ = viper.BindPFlag(envKeyDisableLogColor, pflag.Lookup(envKeyDisableLogColor))

	pflag.Bool(envKeyDisableRedact, false, "Disable redacting sensitive information from logs")
	_ = viper.BindPFlag(envKeyDisableRedact, pflag.Lookup(envKeyDisableRedact))

	pflag.Bool(envKeyForceHTTPS, false, "Force Stash-VR to use HTTPS")
	_ = viper.BindPFlag(envKeyForceHTTPS, pflag.Lookup(envKeyForceHTTPS))

	pflag.String(envKeyBasePath, "", "Path prefix when served under a sub-path behind a reverse proxy, e.g. /stashvr")
	_ = viper.BindPFlag(envKeyBasePath, pflag.Lookup(envKeyBasePath))

	pflag.Bool(envKeyDeovrAutoload, true, "Send the DeoVR library when DeoVR's browser opens the front page")
	_ = viper.BindPFlag(envKeyDeovrAutoload, pflag.Lookup(envKeyDeovrAutoload))

	pflag.Bool(envKeyPerformerFacets, true, "Add Country and Age tags derived from a scene's performers in HereSphere")
	_ = viper.BindPFlag(envKeyPerformerFacets, pflag.Lookup(envKeyPerformerFacets))

	pflag.Bool(envKeyDateLookup, true, "Look up missing release dates from the stash-boxes configured in Stash")
	_ = viper.BindPFlag(envKeyDateLookup, pflag.Lookup(envKeyDateLookup))

	pflag.Bool(envKeyDateWriteback, false, "Write release dates found on stash-boxes back to Stash")
	_ = viper.BindPFlag(envKeyDateWriteback, pflag.Lookup(envKeyDateWriteback))

	pflag.String(envKeyFunscriptIndexPath, "", "Path to the timestampTrade plugin's funscript_index.sqlite; its scripts are offered as alternates")
	_ = viper.BindPFlag(envKeyFunscriptIndexPath, pflag.Lookup(envKeyFunscriptIndexPath))

	pflag.Int(envKeyHeatmapHeightPx, 0, "Height of heatmaps")
	_ = viper.BindPFlag(envKeyHeatmapHeightPx, pflag.Lookup(envKeyHeatmapHeightPx))

	pflag.Int(envKeySmartSectionSize, 50, "Number of scenes in each smart section")
	_ = viper.BindPFlag(envKeySmartSectionSize, pflag.Lookup(envKeySmartSectionSize))

	pflag.Int(envKeyAutoStudioMin, 0, "Generate a section for every studio with at least this many scenes (0 turns it off)")
	_ = viper.BindPFlag(envKeyAutoStudioMin, pflag.Lookup(envKeyAutoStudioMin))

	pflag.Int(envKeyAutoPerformerMin, 0, "Generate a section for every performer with at least this many scenes (0 turns it off)")
	_ = viper.BindPFlag(envKeyAutoPerformerMin, pflag.Lookup(envKeyAutoPerformerMin))

	pflag.String(envKeyExcludeSortName, "hidden", "Exclude tags with this sort name")
	_ = viper.BindPFlag(envKeyExcludeSortName, pflag.Lookup(envKeyExcludeSortName))

	pflag.String(envKeyUserConfigPath, "", "Path to store user config (may contain filter names in plain text)")
	_ = viper.BindPFlag(envKeyUserConfigPath, pflag.Lookup(envKeyUserConfigPath))

	pflag.String(envKeyGenerateSummaryIds, "", "Generate summary ids for categorized tags")
	_ = viper.BindPFlag(envKeyGenerateSummaryIds, pflag.Lookup(envKeyGenerateSummaryIds))

	pflag.BoolP("help", "h", false, "Display usage information")
	_ = viper.BindPFlag("help", pflag.Lookup("help"))

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
	}

	return Load(seed)
}

func (a ApplicationConfig) Redacted() ApplicationConfig {
	a.StashGraphQLUrl = Redacted(a.StashGraphQLUrl)
	a.StashApiKey = Redacted(a.StashApiKey)
	a.ConfigPath = Redacted(a.ConfigPath)
	return a
}
