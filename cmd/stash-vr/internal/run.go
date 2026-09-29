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
	"stash-vr/internal/util"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// warmupTimeout bounds the startup probe of Stash: the version log and the
// first index build. A Stash that never answers must not hold a goroutine
// for the life of the process.
const warmupTimeout = 5 * time.Minute

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

	if m := config.MigratedRules(); len(m) > 0 {
		log.Info().Strs("tags", m).Msg("Dropped default video rules keyed on studio labels; measured tags from vrQualityTags replace them")
	}
	log.Info().Str("config", fmt.Sprintf("%+v", config.Application().Redacted())).Send()

	stashClient := stash.NewClient(config.Application().StashGraphQLUrl, config.Application().StashApiKey)
	libraryService := library.NewService(stashClient)

	return serve(ctx, config.Application().ListenAddress, stashClient, libraryService)
}

// serve runs the HTTP server until ctx ends. Stash is probed and the index
// prebuilt in the background once the server is up, so the Setup page is
// reachable while a slow Stash indexes (or while Stash is not there yet and
// needs to be configured).
func serve(ctx context.Context, listenAddress string, stashClient graphql.Client, libraryService *library.Service) error {
	// Started either way: it idles until an index exists and walks it after
	// every build.
	libraryService.StartDateSweeper(ctx)

	go warmup(ctx, stashClient, libraryService)

	if err := server.Listen(ctx, listenAddress, libraryService); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// warmup logs the versions and prebuilds the index, under warmupTimeout.
func warmup(ctx context.Context, stashClient graphql.Client, libraryService *library.Service) {
	ctx, cancel := context.WithTimeout(ctx, warmupTimeout)
	defer cancel()
	defer util.RecoverLog(ctx, "startup warmup")

	logVersions(ctx, stashClient)

	// Shutting down, or Stash took the whole timeout to answer the version
	// probe: the first request builds the index instead.
	if ctx.Err() != nil {
		log.Ctx(ctx).Warn().Err(ctx.Err()).Msg("library warmup skipped, continuing without prebuilt index")
		return
	}
	if err := libraryService.Warmup(ctx); err != nil {
		// The systemd unit restarts on exit; a slow or briefly unavailable Stash
		// must not turn into a restart loop. The first request will retry.
		log.Ctx(ctx).Warn().Err(err).Msg("library warmup failed, continuing without prebuilt index")
	}
}

func logVersions(ctx context.Context, client graphql.Client) {
	log.Info().Str("Stash-VR version", build.FullVersion()).Send()

	if version, err := stash.GetVersion(ctx, client); err != nil {
		log.Warn().Err(err).Msg("Failed to retrieve stash version")
	} else {
		log.Info().Str("Stash version", version).Send()
	}
}
