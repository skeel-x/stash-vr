package web

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
)

// ApplyChanges performs the side effects of a settings change: a new Stash
// client when the connection changed, a rebuilt index when an auto section
// threshold changed, fresh cover renders when the cover badges changed, a
// new effective log level when the level changed.
func ApplyChanges(prev, next config.ApplicationConfig, lib *library.Service) {
	if prev.StashGraphQLUrl != next.StashGraphQLUrl || prev.StashApiKey != next.StashApiKey {
		lib.SetStashClient(stash.NewClient(next.StashGraphQLUrl, next.StashApiKey))
		log.Info().Msg("Stash connection settings changed, caches cleared")
	}
	if prev.AutoStudioMin != next.AutoStudioMin || prev.AutoPerformerMin != next.AutoPerformerMin {
		lib.ResetSections()
		log.Info().Int("studio_min", next.AutoStudioMin).Int("performer_min", next.AutoPerformerMin).Msg("Auto section thresholds changed, index will be rebuilt")
	}
	if prev.CoverBadges != next.CoverBadges {
		coverbadge.ResetCache()
		log.Info().Msg("Cover badges changed, rendered covers cleared")
	}
	if prev.LogLevel != next.LogLevel {
		// Reassigning the global logger would race with requests that are
		// logging right now; zerolog's global level is an atomic store.
		lvl, err := zerolog.ParseLevel(next.LogLevel)
		if err != nil {
			log.Warn().Err(err).Str("level", next.LogLevel).Msg("Unknown log level, keeping the current one")
			return
		}
		zerolog.SetGlobalLevel(lvl)
		log.Info().Str("level", next.LogLevel).Msg("Log level changed")
	}
}
