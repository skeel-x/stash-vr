package heresphere

import (
	"context"
	"testing"
	"time"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestBuildVideoData_ThumbnailAlwaysUsesCoverEndpoint(t *testing.T) {
	sp := &gql.SceneParts{
		Id:         "9",
		Title:      util.Ptr("Nine"),
		Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:      []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
		Paths: &gql.ScenePartsPathsScenePathsType{
			Screenshot: util.Ptr("http://stash/scene/9/screenshot"),
			Stream:     util.Ptr("http://stash/scene/9/stream"),
		},
	}
	vd := &library.VideoData{SceneParts: sp}

	dto, err := buildVideoData(context.Background(), vd, "https://vr.example")
	if err != nil {
		t.Fatal(err)
	}

	if dto.ThumbnailImage == nil || *dto.ThumbnailImage != "https://vr.example/cover/9" {
		t.Fatalf("expected cover endpoint thumbnail, got %v", dto.ThumbnailImage)
	}
}
