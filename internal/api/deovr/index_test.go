package deovr

import (
	"testing"
	"time"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestBuildIndex_ThumbnailsUseCoverEndpoint(t *testing.T) {
	vd := &library.VideoData{SceneParts: &gql.SceneParts{
		Id:         "9",
		Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:      []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100}},
		Paths:      &gql.ScenePartsPathsScenePathsType{Screenshot: util.Ptr("http://stash/scene/9/screenshot")},
	}}

	index, err := buildIndex([]library.Section{{Name: "A", Ids: []string{"9"}}}, map[string]*library.VideoData{"9": vd}, "https://vr.example")
	if err != nil {
		t.Fatal(err)
	}

	got := index.Scenes[0].List[0].ThumbnailUrl
	if got == nil || *got != "https://vr.example/cover/9" {
		t.Fatalf("expected cover endpoint thumbnail, got %v", got)
	}
}
