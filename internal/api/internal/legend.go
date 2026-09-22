package internal

var (
	LegendTag       = "#"
	LegendPerformer = "@"

	LegendSceneStudio = "Studio"
	LegendSceneGroup  = "%"

	LegendMetaOCount      = "O-Count"
	LegendMetaOrganized   = "Organized"
	LegendMetaPlayCount   = "Played"
	LegendMetaResolution  = "Resolution"
	LegendMetaRating      = "Rating"
	LegendMetaInteractive = "Interactive"

	LegendSummary   = "Summary"
	LegendSummaryId = "SummaryId"

	CommandIncrementO       = "/o"
	CommandSetOrganizedTrue = "/org"
)

// legacyLegends are prefixes older stash-vr releases used. HereSphere may
// still send them back from its cache; they are metadata, never markers.
var legacyLegends = map[string]struct{}{
	"P": {}, "Org": {}, "O": {}, "movie": {}, "Movie": {}, "Performer": {}, "Tag": {}, "?": {}, "Σ": {},
}

// IsLegacyLegend reports whether key was a legend in an earlier release.
func IsLegacyLegend(key string) bool {
	_, ok := legacyLegends[key]
	return ok
}

var (
	TagVR_DOME    = "DOME"
	TagVR_SPHERE  = "SPHERE"
	TagVR_FISHEYE = "FISHEYE"
	TagVR_MKX200  = "MKX200"
	TagVR_RF52    = "RF52"
	TagVR_SBS     = "SBS"
	TagVR_TB      = "TB"

	TagVR_CUBEMAP = "CUBEMAP"
	TagVR_EAC     = "EAC"
)
