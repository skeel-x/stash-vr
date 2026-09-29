package heresphere

import (
	"context"
	"testing"
	"time"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func sceneWithFile(id string, duration float64) *library.VideoData {
	return &library.VideoData{SceneParts: &gql.SceneParts{
		Id:         id,
		Title:      util.Ptr("Scene " + id),
		Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:      []*gql.ScenePartsFilesVideoFile{{Basename: id + ".mp4", Duration: duration, Height: 1080}},
		Paths:      &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/" + id + "/stream")},
	}}
}

func TestBuildScan_SkipsScenesWithoutAFile(t *testing.T) {
	loadDefaultRules(t)
	vds := map[string]*library.VideoData{
		"1": sceneWithFile("1", 100),
		"2": nil,
		"3": {SceneParts: nil},
		"4": {SceneParts: &gql.SceneParts{Id: "4", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
		"5": sceneWithFile("5", 200),
	}

	doc, err := buildScan(context.Background(), vds, "https://vr.example")
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, s := range doc.ScanData {
		got[s.id] = true
	}
	if len(got) != 2 || !got["1"] || !got["5"] {
		t.Fatalf("expected only the scenes with a file, got %v", got)
	}
}

func TestScanDuration_MatchesVideoDataDuration(t *testing.T) {
	loadDefaultRules(t)
	vd := sceneWithFile("7", 1234.5)

	scan := videoDataToScanDataDto(context.Background(), vd, "https://vr.example")
	doc, err := buildVideoData(context.Background(), vd, "https://vr.example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if scan.Duration != doc.Duration {
		t.Fatalf("scan duration %v differs from scene document duration %v", scan.Duration, doc.Duration)
	}
	if scan.Duration != 1234500 {
		t.Fatalf("expected milliseconds, got %v", scan.Duration)
	}
}

func TestGetTags_ToleratesMissingFile(t *testing.T) {
	loadDefaultRules(t)
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "4", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}}

	tags := getTags(vd)

	if len(tags) == 0 {
		t.Fatal("expected the metadata tags even without a file")
	}
	for _, tag := range tags {
		if tag.Name == "Resolution:0p" {
			t.Fatal("a scene without a file must not report a resolution")
		}
	}
	if fields := getFields(vd); len(fields) == 0 {
		t.Fatal("expected fields without a file")
	}
}
