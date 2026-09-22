package web

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/build"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/stash/gql"
)

const (
	ConnectionOk           = "ok"
	ConnectionUnauthorized = "unauthorized"
	ConnectionUnreachable  = "unreachable"
)

// Status is what the Players page and GET /api/ui/status show.
type Status struct {
	Version          string `json:"version"`
	StashVersion     string `json:"stash_version,omitempty"`
	Connection       string `json:"connection"`
	ConnectionError  string `json:"connection_error,omitempty"`
	ListenAddress    string `json:"listen_address"`
	ConfigPath       string `json:"config_path"`
	ConfigFileExists bool   `json:"config_file_exists"`
	ApiKeySet        bool   `json:"api_key_set"`
	LogLevel         string `json:"log_level"`
	Sections         int    `json:"sections"`
	Links            int    `json:"links"`
	Scenes           int    `json:"scenes"`
	// SampleCoverUrl carries the Stash API key (via stash.ApiKeyed) so the
	// Players page's headset-reachability check can load it directly. It must
	// never be serialised to the browser.
	SampleCoverUrl string `json:"-"`
}

// BuildStatus probes Stash and the library. It never returns an error: every
// failure is reported inside the status so the page can explain it.
func BuildStatus(ctx context.Context, lib *library.Service) Status {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cfg := config.Application()
	s := Status{
		Version:       build.FullVersion(),
		ListenAddress: cfg.ListenAddress,
		ConfigPath:    config.FilePath(cfg),
		ApiKeySet:     cfg.StashApiKey != "",
		LogLevel:      cfg.LogLevel,
	}
	if _, err := os.Stat(s.ConfigPath); err == nil {
		s.ConfigFileExists = true
	}

	version, err := stash.GetVersion(probeCtx, lib.Client())
	if err != nil {
		var httpErr *graphql.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
			s.Connection = ConnectionUnauthorized
		} else {
			s.Connection = ConnectionUnreachable
		}
		s.ConnectionError = describeStashError(err)
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to retrieve stash version")
		return s
	}
	s.Connection = ConnectionOk
	s.StashVersion = version

	// GetSections runs its build under a singleflight shared with player index
	// requests (HereSphere/DeoVR); the probe timeout must not govern that
	// shared build, so the original request context is used here, not
	// probeCtx.
	sections, err := lib.GetSections(ctx)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to retrieve sections")
	} else {
		s.Sections = len(sections)
		stats := lib.StatsSnapshot()
		s.Links = stats.Links
		s.Scenes = stats.Scenes
	}

	cover, err := gql.FindSampleSceneCover(probeCtx, lib.Client())
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to retrieve sample scene cover url")
	} else if cover.FindScenes != nil && len(cover.FindScenes.Scenes) > 0 && cover.FindScenes.Scenes[0].Paths != nil && cover.FindScenes.Scenes[0].Paths.Screenshot != nil {
		s.SampleCoverUrl = stash.ApiKeyed(*cover.FindScenes.Scenes[0].Paths.Screenshot)
	}
	return s
}
