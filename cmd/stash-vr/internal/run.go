package internal

import (
	"context"
	"errors"
	"fmt"
	"stash-vr/internal/build"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/logger"
	"stash-vr/internal/server"
	"stash-vr/internal/stash"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func Run(ctx context.Context) error {
	var seedNotPersisted error
	if err := config.Init(); err != nil {
		if !errors.Is(err, config.ErrSeedNotPersisted) {
			return fmt.Errorf("config: %w", err)
		}
		// The settings are in memory and the service is usable; only the
		// config file is missing. Warn once the logger exists.
		seedNotPersisted = err
	}
	log.Logger = logger.New(config.Application().LogLevel, config.Application().DisableLogColor)
	zerolog.DefaultContextLogger = &log.Logger
	if seedNotPersisted != nil {
		log.Warn().Err(seedNotPersisted).Msg("settings will not persist; check CONFIG_PATH permissions")
	}

	log.Info().Str("config", fmt.Sprintf("%+v", config.Application().Redacted())).Send()

	stashClient := stash.NewClient(config.Application().StashGraphQLUrl, config.Application().StashApiKey)
	logVersions(ctx, stashClient)

	libraryService := library.NewService(stashClient)
	if err := libraryService.Warmup(ctx); err != nil {
		// The systemd unit restarts on exit; a slow or briefly unavailable Stash
		// must not turn into a restart loop. The first request will retry.
		log.Ctx(ctx).Warn().Err(err).Msg("library warmup failed, continuing without prebuilt index")
	}
	// Started either way: it idles until an index exists and walks it after
	// every build.
	libraryService.StartDateSweeper(ctx)

	err := server.Listen(ctx, config.Application().ListenAddress, libraryService)
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}

func logVersions(ctx context.Context, client graphql.Client) {
	log.Info().Str("Stash-VR version", build.FullVersion()).Send()

	if version, err := stash.GetVersion(ctx, client); err != nil {
		log.Warn().Err(err).Msg("Failed to retrieve stash version")
	} else {
		log.Info().Str("Stash version", version).Send()
	}
}
