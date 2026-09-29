package deovr

import (
	"testing"
	"time"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
)

func TestBuildIndex_SkipsBadEntriesPerSection(t *testing.T) {
	good := &library.VideoData{SceneParts: &gql.SceneParts{
		Id:         "1",
		Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:      []*gql.ScenePartsFilesVideoFile{{Basename: "one.mp4", Duration: 100}},
	}}
	vds := map[string]*library.VideoData{
		"1": good,
		"2": nil,
		"3": {SceneParts: nil},
		"4": {SceneParts: &gql.SceneParts{Id: "4"}},
		"5": {SceneParts: &gql.SceneParts{Id: "5", Files: []*gql.ScenePartsFilesVideoFile{nil}}},
	}
	sections := []library.Section{
		{Name: "A", Ids: []string{"2", "1", "3", "4", "5", "missing"}},
		{Name: "B", Ids: []string{"missing"}},
	}

	index, err := buildIndex(sections, vds, "https://vr.example")
	if err != nil {
		t.Fatal(err)
	}

	if len(index.Scenes) != 2 {
		t.Fatalf("expected both sections, got %d", len(index.Scenes))
	}
	if a := index.Scenes[0].List; len(a) != 1 || a[0].Id != "1" || a[0].VideoLength != 100 {
		t.Fatalf("section A must keep only the scene with a file, got %+v", a)
	}
	if b := index.Scenes[1].List; len(b) != 0 {
		t.Fatalf("section B must be empty, got %+v", b)
	}
}
