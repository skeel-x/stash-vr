package playa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func loadKeyedConfig(t *testing.T) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", StashApiKey: "secret", FavoriteTag: "FAVORITE",
		LogLevel: "debug", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
}

// captureLog routes the global logger into a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = prev })
	return &buf
}

func TestBuildVideoLinks_LogNeverCarriesApiKey(t *testing.T) {
	loadKeyedConfig(t)
	buf := captureLog(t)
	sp := &gql.SceneParts{
		Id:    "7",
		Files: []*gql.ScenePartsFilesVideoFile{{Basename: "seven.mp4", Duration: 100, Height: 1080}},
		Paths: &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash:9999/scene/7/stream")},
		SceneStreams: []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{
			{Url: "http://stash:9999/scene/7/stream.m3u8?resolution=STANDARD", Mime_type: util.Ptr("application/vnd.apple.mpegurl"), Label: util.Ptr("HLS (720p)")},
		},
	}

	links := buildVideoLinks(&library.VideoData{SceneParts: sp}, false)

	if len(links) < 2 || links[0].URL == nil || !strings.Contains(*links[0].URL, "apikey=secret") {
		t.Fatalf("the links themselves must stay keyed, got %+v", links)
	}
	logged := buf.String()
	if !strings.Contains(logged, "Generated video links") {
		t.Fatalf("expected the links debug line, got %q", logged)
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("api key leaked into the log: %s", logged)
	}
	if !strings.Contains(logged, "apikey=REDACTED") {
		t.Fatalf("expected redacted urls in the log, got %s", logged)
	}
}

func TestRedactedLinks(t *testing.T) {
	keyed := "http://stash:9999/scene/1/stream?apikey=secret"
	in := []VideoLinkView{
		{URL: &keyed, QualityName: "1080p"},
		{URL: nil, UnavailableReason: util.Ptr("offline")},
	}

	out := redactedLinks(in)

	if len(out) != 2 || out[0].URL == nil || *out[0].URL != "http://stash:9999/scene/1/stream?apikey=REDACTED" || out[0].QualityName != "1080p" {
		t.Fatalf("unexpected redaction %+v", out)
	}
	if out[1].URL != nil || out[1].UnavailableReason == nil {
		t.Fatalf("a link without a url must be copied as is, got %+v", out[1])
	}
	if *in[0].URL != keyed {
		t.Fatal("the original links must not be changed")
	}
}
