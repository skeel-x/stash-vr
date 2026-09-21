package web

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/logger"
	"stash-vr/internal/stash"
)

// ApplyChanges performs the side effects of a settings change: a new Stash
// client when the connection changed, a new logger when the level changed.
func ApplyChanges(prev, next config.ApplicationConfig, lib *library.Service) {
	if prev.StashGraphQLUrl != next.StashGraphQLUrl || prev.StashApiKey != next.StashApiKey {
		lib.SetStashClient(stash.NewClient(next.StashGraphQLUrl, next.StashApiKey))
		log.Info().Msg("Stash connection settings changed, caches cleared")
	}
	if prev.LogLevel != next.LogLevel {
		log.Logger = logger.New(next.LogLevel, next.DisableLogColor)
		zerolog.DefaultContextLogger = &log.Logger
		log.Info().Str("level", next.LogLevel).Msg("Log level changed")
	}
}
