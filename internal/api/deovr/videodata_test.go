package deovr

import (
	"strings"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestBuildVideoData_ToleratesMissingParts(t *testing.T) {
	file := &gql.ScenePartsFilesVideoFile{Basename: "nine.mp4", Duration: 100, Height: 1080}
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		vd      *library.VideoData
		wantErr bool
	}{
		{name: "nil scene", vd: nil, wantErr: true},
		{name: "no scene parts", vd: &library.VideoData{}, wantErr: true},
		{name: "no files", vd: &library.VideoData{SceneParts: &gql.SceneParts{Id: "9", Created_at: created}}, wantErr: true},
		{name: "nil first file", vd: &library.VideoData{SceneParts: &gql.SceneParts{Id: "9", Created_at: created, Files: []*gql.ScenePartsFilesVideoFile{nil}}}, wantErr: true},
		{name: "no paths or streams", vd: &library.VideoData{SceneParts: &gql.SceneParts{Id: "9", Created_at: created, Files: []*gql.ScenePartsFilesVideoFile{file},
			SceneStreams: []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{nil}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dto, err := buildVideoData(c.vd, "https://vr.example")
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", dto)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dto.Id != "9" || dto.VideoLength != 100 || dto.ThumbnailUrl != nil || dto.VideoPreview != nil {
				t.Fatalf("dto = %+v", dto)
			}
			if len(dto.Encodings) != 2 || len(dto.Encodings[0].VideoSources) != 0 || len(dto.Encodings[1].VideoSources) != 0 {
				t.Fatalf("encodings = %+v", dto.Encodings)
			}
		})
	}
}

// See TestSetMediaSources_KeysSignedStreams in the heresphere package: the
// signed urls Stash 0.32 returns expire, the key does not.
func TestSetStreamSources_KeysSignedStreams(t *testing.T) {
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", StashApiKey: "secret", FavoriteTag: "FAVORITE",
		LogLevel: "debug", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	sp := &gql.SceneParts{Id: "9",
		Files: []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
		Paths: &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream?cid=c&expires=1&signature=s")},
		SceneStreams: []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{
			{Url: "http://stash/scene/9/stream.mp4?cid=c&expires=1&resolution=STANDARD&signature=s", Mime_type: util.Ptr("video/mp4"), Label: util.Ptr("MP4 (1080p)")},
		},
	}
	var dto videoDataDto
	setStreamSources(&library.VideoData{SceneParts: sp}, &dto)
	if len(dto.Encodings) != 2 || len(dto.Encodings[0].VideoSources) != 1 || len(dto.Encodings[1].VideoSources) != 1 {
		t.Fatalf("encodings = %+v", dto.Encodings)
	}
	for _, e := range dto.Encodings {
		u := e.VideoSources[0].Url
		if !strings.Contains(u, "signature=s") || !strings.Contains(u, "&apikey=secret") {
			t.Fatalf("%s stream must keep the signature and add the key, got %q", e.Name, u)
		}
	}
}
