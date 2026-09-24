package heresphere

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func loadDefaultRules(t *testing.T) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", FavoriteTag: "FAVORITE",
		LogLevel: "info", ExcludeSortName: "hidden", SmartSectionSize: 50, PerformerFacets: true, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildVideoData_FormatFromRules(t *testing.T) {
	loadDefaultRules(t)
	sp := &gql.SceneParts{Id: "9", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:         []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
		Paths:         &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream")},
		TagPartsArray: gql.TagPartsArray{Tags: []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: "MKX200"}}, {TagParts: gql.TagParts{Id: "2", Name: "TB"}}, {TagParts: gql.TagParts{Id: "3", Name: "Alpha"}}}},
	}
	dto, err := buildVideoData(context.Background(), &library.VideoData{SceneParts: sp}, "https://vr.example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Projection != "fisheye" || dto.Stereo != "tb" || dto.Lens != "MKX200" || dto.Fov != 200 {
		t.Fatalf("format = %s/%s/%s/%v", dto.Projection, dto.Stereo, dto.Lens, dto.Fov)
	}
	if dto.AlphaPackedSettings == nil || !dto.AlphaPackedSettings.DefaultSettings {
		t.Fatal("expected alphaPackedSettings for an Alpha scene")
	}
	if dto.WriteHSP == nil || !*dto.WriteHSP {
		t.Fatal("expected writeHSP on")
	}
	b, _ := json.Marshal(dto)
	if !strings.Contains(string(b), `"alphaPackedSettings":{"defaultSettings":true}`) || strings.Contains(string(b), `"hsp"`) {
		t.Fatalf("unexpected JSON %s", b)
	}
}

func TestBuildVideoData_NoAlphaWithoutPassthrough(t *testing.T) {
	loadDefaultRules(t)
	sp := &gql.SceneParts{Id: "9", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files: []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4"}}, Paths: &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream")}, TagPartsArray: gql.TagPartsArray{Tags: []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: "DOME"}}, {TagParts: gql.TagParts{Id: "2", Name: "Passthrough"}}, {TagParts: gql.TagParts{Id: "3", Name: "Augmented Reality"}}}}}
	dto, err := buildVideoData(context.Background(), &library.VideoData{SceneParts: sp}, "https://vr.example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dto.AlphaPackedSettings != nil || dto.Projection != "equirectangular" || dto.Stereo != "sbs" {
		t.Fatalf("got %+v", dto)
	}
}

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

	dto, err := buildVideoData(context.Background(), vd, "https://vr.example", nil, nil)
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

type fakeProfiles map[string]bool

func (f fakeProfiles) HasProfile(id string) bool { return f[id] }

func TestBuildVideoData_ProfileLinkPrecedence(t *testing.T) {
	loadDefaultRules(t)
	cfg := config.Application()
	cfg.VideoRules = append(cfg.VideoRules, config.VideoRule{Tag: "Passthrough", Profile: "11649"})
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	scene := func(id string) *library.VideoData {
		return &library.VideoData{SceneParts: &gql.SceneParts{Id: id, Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Files:         []*gql.ScenePartsFilesVideoFile{{Basename: "x.mp4"}},
			Paths:         &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/" + id + "/stream")},
			TagPartsArray: gql.TagPartsArray{Tags: []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: "Passthrough"}}}}}}
	}
	cases := []struct {
		name     string
		id       string
		profiles fakeProfiles
		want     string
	}{
		{"own profile wins", "9", fakeProfiles{"9": true, "11649": true}, "https://vr.example/hsp/scene/9"},
		{"rule profile", "9", fakeProfiles{"11649": true}, "https://vr.example/hsp/scene/11649"},
		{"none stored", "9", fakeProfiles{}, ""},
	}
	for _, c := range cases {
		dto, err := buildVideoData(context.Background(), scene(c.id), "https://vr.example", nil, c.profiles)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if dto.Hsp != nil {
			got = *dto.Hsp
		}
		if got != c.want {
			t.Errorf("%s: hsp = %q want %q", c.name, got, c.want)
		}
	}
}
