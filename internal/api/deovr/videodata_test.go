package deovr

import (
	"testing"
	"time"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
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
