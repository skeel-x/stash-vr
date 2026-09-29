package stash

import (
	"testing"

	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestGetDirectStream_ToleratesMissingParts(t *testing.T) {
	file := &gql.ScenePartsFilesVideoFile{Basename: "seven.mp4", Height: 1080}
	paths := &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/7/stream")}
	cases := []struct {
		name    string
		sp      *gql.SceneParts
		wantUrl string
		wantRes int
	}{
		{name: "nil scene", sp: nil},
		{name: "no files", sp: &gql.SceneParts{Paths: paths}},
		{name: "nil first file", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{nil}, Paths: paths}},
		{name: "no paths", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}}},
		{name: "no stream path", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}, Paths: &gql.ScenePartsPathsScenePathsType{}}},
		{name: "empty stream path", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}, Paths: &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("")}}},
		{name: "complete", sp: &gql.SceneParts{Files: []*gql.ScenePartsFilesVideoFile{file}, Paths: paths}, wantUrl: "http://stash/scene/7/stream", wantRes: 1080},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := GetDirectStream(c.sp)
			if s.Name != "direct" {
				t.Fatalf("name = %q", s.Name)
			}
			if c.wantUrl == "" {
				if len(s.Sources) != 0 {
					t.Fatalf("expected no sources, got %+v", s.Sources)
				}
				return
			}
			if len(s.Sources) != 1 || s.Sources[0].Url != c.wantUrl || s.Sources[0].Resolution != c.wantRes {
				t.Fatalf("sources = %+v", s.Sources)
			}
		})
	}
}

func TestTranscodingStream_SkipsIncompleteEntries(t *testing.T) {
	mp4 := util.Ptr("video/mp4")
	hls := util.Ptr("application/vnd.apple.mpegurl")
	sp := &gql.SceneParts{
		Files: []*gql.ScenePartsFilesVideoFile{{Height: 720}},
		SceneStreams: []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{
			nil,
			{Url: "http://stash/no-mime", Label: util.Ptr("MP4 (1080p)")},
			{Url: "http://stash/no-label", Mime_type: mp4},
			{Url: "http://stash/direct", Mime_type: mp4, Label: util.Ptr("Direct stream")},
			{Url: "http://stash/hls", Mime_type: hls, Label: util.Ptr("HLS (480p)")},
			{Url: "http://stash/mp4-480", Mime_type: mp4, Label: util.Ptr("MP4 (480p)")},
			{Url: "http://stash/mp4-unlabelled", Mime_type: mp4, Label: util.Ptr("MP4")},
			{Url: "http://stash/mp4-480-again", Mime_type: mp4, Label: util.Ptr("MP4 (480p)")},
		},
	}

	got := GetTranscodingStream(sp)
	if got.Name != "transcoding" {
		t.Fatalf("name = %q", got.Name)
	}
	// The unlabelled transcode falls back to the file height (720) and
	// sorts first; the repeated 480p is dropped.
	want := []Source{{Resolution: 720, Url: "http://stash/mp4-unlabelled"}, {Resolution: 480, Url: "http://stash/mp4-480"}}
	if len(got.Sources) != len(want) {
		t.Fatalf("sources = %+v, want %+v", got.Sources, want)
	}
	for i := range want {
		if got.Sources[i] != want[i] {
			t.Fatalf("source %d = %+v, want %+v", i, got.Sources[i], want[i])
		}
	}

	if h := GetHLSStream(sp); len(h.Sources) != 1 || h.Sources[0].Url != "http://stash/hls" || h.Sources[0].Resolution != 480 {
		t.Fatalf("hls sources = %+v", h.Sources)
	}
}

func TestTranscodingStream_NoFilesOrScene(t *testing.T) {
	// An unlabelled transcode of a scene without a file gets resolution 0
	// rather than a nil dereference.
	sp := &gql.SceneParts{SceneStreams: []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{
		{Url: "http://stash/mp4", Mime_type: util.Ptr("video/mp4"), Label: util.Ptr("MP4")},
	}}
	if got := GetTranscodingStream(sp); len(got.Sources) != 1 || got.Sources[0].Resolution != 0 {
		t.Fatalf("sources = %+v", got.Sources)
	}
	for _, s := range []Stream{GetTranscodingStream(nil), GetHLSStream(nil)} {
		if s.Sources == nil || len(s.Sources) != 0 {
			t.Fatalf("expected an empty source list for a nil scene, got %+v", s.Sources)
		}
	}
}
