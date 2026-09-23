package heresphere

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/util"
)

// incomingTags is the classification of the tag list HereSphere writes back.
type incomingTags struct {
	tags                                             []string            // Stash tags to set ("#:name")
	markers                                          []library.MarkerDto // timed tags to store as markers
	commands                                         []string            // "/o", "/org"
	hasPlayCount, hasOrganized, hasOCount, hasRating bool
}

// isMarker reports whether a tag is a marker: it carries a Stash marker id
// (sent by us in the rating field) or a time span. Plain tags have neither;
// hidden tags use -1.
func isMarker(t tagDto) bool {
	if t.Rating != nil {
		return true
	}
	return t.Start >= 0 && (t.End != nil || t.Start > 0)
}

// markerID recovers the Stash marker id transported in the rating field;
// a tag created in HereSphere has none, which means "new".
func markerID(rating *float32) string {
	if rating == nil {
		return "0"
	}
	return fmt.Sprintf("%.0f", *rating)
}

// classifyIncomingTags sorts HereSphere's tags into Stash tags, markers,
// commands and metadata flags. Anything with an unknown prefix that carries
// no time span, and anything with a legacy prefix, is ignored: those are
// metadata, not markers.
func classifyIncomingTags(ctx context.Context, tags []tagDto) incomingTags {
	in := incomingTags{tags: []string{}, markers: []library.MarkerDto{}}
	for _, t := range tags {
		key, arg, _ := strings.Cut(t.Name, ":")
		if key == "" {
			continue
		}
		// A rating is only ever set on markers we sent out (it carries the
		// Stash marker id), so it wins over every legend match: a marker whose
		// primary tag happens to be named "O" or "Studio" must survive the
		// round trip, or UpdateMarkers would destroy it.
		if t.Rating != nil {
			in.markers = append(in.markers, newMarker(key, arg, t))
			continue
		}
		switch key {
		case internal.LegendPerformer, internal.LegendPerformerCountry, internal.LegendPerformerAge,
			internal.LegendSceneStudio, internal.LegendSceneGroup,
			internal.LegendMetaResolution, internal.LegendSummary, internal.LegendSummaryId,
			internal.LegendMetaInteractive, internal.LegendMetaWatched, internal.LegendMetaResume, internal.LegendMetaReleased:
			continue
		case internal.LegendMetaOCount:
			in.hasOCount = true
			continue
		case internal.LegendMetaOrganized:
			in.hasOrganized = true
			continue
		case internal.LegendMetaPlayCount:
			in.hasPlayCount = true
			continue
		case internal.LegendMetaRating:
			in.hasRating = true
			continue
		}
		if strings.HasPrefix(key, internal.LegendTag) {
			if key == internal.LegendTag && arg != "" && arg[0] != '#' {
				in.tags = append(in.tags, arg)
			}
			continue
		}
		if strings.EqualFold(key, internal.CommandIncrementO) || strings.EqualFold(key, internal.CommandSetOrganizedTrue) {
			in.commands = append(in.commands, strings.ToLower(key))
			continue
		}
		if internal.IsLegacyLegend(key) {
			log.Ctx(ctx).Debug().Str("tag", t.Name).Msg("Ignoring tag with a legacy legend")
			continue
		}
		if !isMarker(t) {
			log.Ctx(ctx).Debug().Str("tag", t.Name).Msg("Ignoring untimed tag with an unknown legend")
			continue
		}
		in.markers = append(in.markers, newMarker(key, arg, t))
	}
	return in
}

// newMarker builds the marker a HereSphere tag describes; times arrive in ms.
func newMarker(key, arg string, t tagDto) library.MarkerDto {
	m := library.MarkerDto{
		PrimaryTagName: key,
		StartSecond:    t.Start / 1000,
		MarkerId:       markerID(t.Rating),
		Title:          arg,
	}
	if t.End != nil {
		m.EndSecond = util.Ptr(*t.End / 1000)
	}
	return m
}
