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

	dto, err := buildVideoData(context.Background(), vd, "https://vr.example", nil)
	if err != nil {
		t.Fatal(err)
	}

	if dto.ThumbnailImage == nil || *dto.ThumbnailImage != "https://vr.example/cover/9" {
		t.Fatalf("expected cover endpoint thumbnail, got %v", dto.ThumbnailImage)
	}
}

func TestSetScripts_StandardFromStashThenVariantsFromStashVr(t *testing.T) {
	sp := &gql.SceneParts{
		Id:          "9",
		Interactive: true,
		Files:       []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Path: "/v/nine.mp4"}},
		Paths:       &gql.ScenePartsPathsScenePathsType{Funscript: util.Ptr("http://stash/scene/9/funscript")},
	}
	vd := &library.VideoData{SceneParts: sp}
	variants := []library.ScriptVariant{
		{Label: "Standard", Path: "/v/nine.funscript"},
		{Label: "AI", Path: "/v/nine.ai.funscript"},
		{Label: "Alternate 1 (Goat)", Path: "/x/other.funscript"},
	}

	var dto videoDataDto
	setScripts(vd, &dto, "https://vr.example", variants)

	if len(dto.Scripts) != 3 {
		t.Fatalf("expected 3 scripts, got %+v", dto.Scripts)
	}
	if dto.Scripts[0].Name != "Standard" || dto.Scripts[0].Url != "http://stash/scene/9/funscript" {
		t.Fatalf("standard = %+v", dto.Scripts[0])
	}
	if dto.Scripts[1].Name != "AI" || dto.Scripts[1].Url != "https://vr.example/funscript/9/1" {
		t.Fatalf("variant = %+v", dto.Scripts[1])
	}
	if dto.Scripts[2].Name != "Alternate 1 (Goat)" || dto.Scripts[2].Url != "https://vr.example/funscript/9/2" {
		t.Fatalf("alternate = %+v", dto.Scripts[2])
	}
}

func TestSetScripts_NoVariantsFallsBackToStashScript(t *testing.T) {
	sp := &gql.SceneParts{
		Id:          "9",
		Interactive: true,
		Paths:       &gql.ScenePartsPathsScenePathsType{Funscript: util.Ptr("http://stash/scene/9/funscript")},
	}
	var dto videoDataDto
	setScripts(&library.VideoData{SceneParts: sp}, &dto, "https://vr.example", nil)

	if len(dto.Scripts) != 1 || dto.Scripts[0].Name != "Standard" || dto.Scripts[0].Url != "http://stash/scene/9/funscript" {
		t.Fatalf("got %+v", dto.Scripts)
	}
}

func TestSetScripts_StandardWithoutStashUrlIsServedByStashVr(t *testing.T) {
	sp := &gql.SceneParts{Id: "9"}
	var dto videoDataDto
	setScripts(&library.VideoData{SceneParts: sp}, &dto, "https://vr.example", []library.ScriptVariant{{Label: "Standard", Path: "/v/nine.funscript"}})

	if len(dto.Scripts) != 1 || dto.Scripts[0].Url != "https://vr.example/funscript/9/0" {
		t.Fatalf("got %+v", dto.Scripts)
	}
}

func TestSetScripts_KeepsStashScriptWhenScanFindsOnlyVariants(t *testing.T) {
	sp := &gql.SceneParts{
		Id:          "9",
		Interactive: true,
		Paths:       &gql.ScenePartsPathsScenePathsType{Funscript: util.Ptr("http://stash/scene/9/funscript")},
	}
	var dto videoDataDto
	setScripts(&library.VideoData{SceneParts: sp}, &dto, "https://vr.example", []library.ScriptVariant{{Label: "AI", Path: "/v/nine.ai.funscript"}})

	if len(dto.Scripts) != 2 || dto.Scripts[0].Name != "Standard" || dto.Scripts[0].Url != "http://stash/scene/9/funscript" || dto.Scripts[1].Url != "https://vr.example/funscript/9/0" {
		t.Fatalf("got %+v", dto.Scripts)
	}
}
