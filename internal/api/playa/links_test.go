package playa

import (
	"testing"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestBuildPlayableLinkCandidates_ToleratesMissingParts(t *testing.T) {
	loadKeyedConfig(t)
	file := &gql.ScenePartsFilesVideoFile{Height: 1080}
	stream := util.Ptr("http://stash:9999/scene/7/stream")
	hls := []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{
		nil,
		{Url: "http://stash:9999/scene/7/stream.m3u8", Mime_type: util.Ptr("application/vnd.apple.mpegurl"), Label: util.Ptr("HLS (720p)")},
	}
	cases := []struct {
		name       string
		sp         *gql.SceneParts
		wantDirect int
		wantOther  int
	}{
		{name: "no paths", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}, SceneStreams: hls}, wantOther: 1},
		{name: "no stream path", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}, Paths: &gql.ScenePartsPathsScenePathsType{}, SceneStreams: hls}, wantOther: 1},
		{name: "no files", sp: &gql.SceneParts{Paths: &gql.ScenePartsPathsScenePathsType{Stream: stream}, SceneStreams: hls}},
		{name: "nil first file", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{nil}, Paths: &gql.ScenePartsPathsScenePathsType{Stream: stream}, SceneStreams: hls}},
		{name: "complete", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}, Paths: &gql.ScenePartsPathsScenePathsType{Stream: stream}, SceneStreams: hls}, wantDirect: 1, wantOther: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildPlayableLinkCandidates(&library.VideoData{SceneParts: c.sp}, "flat", "mono")
			direct, other := 0, 0
			for _, cand := range got {
				if cand.isDirect {
					direct++
				} else {
					other++
				}
			}
			if direct != c.wantDirect || other != c.wantOther {
				t.Fatalf("got %d direct and %d other candidates, want %d and %d: %+v", direct, other, c.wantDirect, c.wantOther, got)
			}
		})
	}
}
