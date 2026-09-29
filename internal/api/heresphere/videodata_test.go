package heresphere

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
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

func TestBuildVideoData_LenslessFisheyeGetsLinearLens(t *testing.T) {
	loadDefaultRules(t)
	sp := &gql.SceneParts{Id: "9", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:         []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
		Paths:         &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream")},
		TagPartsArray: gql.TagPartsArray{Tags: []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: "RF52"}}, {TagParts: gql.TagParts{Id: "2", Name: "SBS"}}}},
	}
	dto, err := buildVideoData(context.Background(), &library.VideoData{SceneParts: sp}, "https://vr.example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The RF52 rule sets fov 190 but no lens; HereSphere ignores the fov
	// unless a lens is named, so it must default to the linear lens.
	if dto.Projection != "fisheye" || dto.Fov != 190 || dto.Lens != "Linear" {
		t.Fatalf("format = %s/%s/%v lens=%q, want fisheye fov=190 lens=Linear", dto.Projection, dto.Stereo, dto.Fov, dto.Lens)
	}
	if b, _ := json.Marshal(dto); !strings.Contains(string(b), `"lens":"Linear"`) {
		t.Fatalf("expected lens in JSON, got %s", b)
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

	if dto.ThumbnailImage == nil || !strings.HasPrefix(*dto.ThumbnailImage, "https://vr.example/cover/9?b=") {
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

func (fakeProfiles) StudioProfile(context.Context, *library.VideoData, *library.Format) string {
	return ""
}

// learnedProfiles also learns the profile of scene learned for every
// scene.
type learnedProfiles struct {
	fakeProfiles
	learned string
}

func (l learnedProfiles) StudioProfile(context.Context, *library.VideoData, *library.Format) string {
	return l.learned
}

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
		profiles profileLookup
		want     string
	}{
		{"own profile wins", "9", fakeProfiles{"9": true, "11649": true}, "https://vr.example/hsp/scene/9"},
		{"own beats studio", "9", learnedProfiles{fakeProfiles{"9": true, "11649": true, "20": true}, "20"}, "https://vr.example/hsp/scene/9"},
		{"studio beats rule", "9", learnedProfiles{fakeProfiles{"11649": true, "20": true}, "20"}, "https://vr.example/hsp/scene/9"},
		{"rule profile", "9", fakeProfiles{"11649": true}, "https://vr.example/hsp/scene/9"},
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

func TestBuildVideoData_VerticalCorrectionLinksGeneratedProfile(t *testing.T) {
	loadDefaultRules(t)
	scene := func(offset any, tag string) *library.VideoData {
		return &library.VideoData{SceneParts: &gql.SceneParts{Id: "9", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Files:         []*gql.ScenePartsFilesVideoFile{{Basename: "x.mp4"}},
			Paths:         &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream")},
			Custom_fields: map[string]any{library.VerticalOffsetField: offset},
			TagPartsArray: gql.TagPartsArray{Tags: []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: tag}}}}}}
	}
	hsp := func(vd *library.VideoData, profiles profileLookup) string {
		dto, err := buildVideoData(context.Background(), vd, "https://vr.example", nil, profiles)
		if err != nil {
			t.Fatal(err)
		}
		if dto.Hsp == nil {
			return ""
		}
		return *dto.Hsp
	}
	for _, on := range []bool{true, false} {
		cfg := config.Application()
		cfg.CorrectVerticalStereo = on
		if _, err := config.Set(cfg); err != nil {
			t.Fatal(err)
		}
		want := ""
		if on {
			want = "https://vr.example/hsp/scene/9"
		}
		if got := hsp(scene(0.8, "DOME"), fakeProfiles{}); got != want {
			t.Errorf("on=%v: hsp = %q want %q", on, got, want)
		}
		if got := hsp(scene(0.8, "FLAT"), fakeProfiles{}); got != "" {
			t.Errorf("on=%v: flat scene got a profile %q", on, got)
		}
		if got := hsp(scene(0.1, "DOME"), fakeProfiles{}); got != "" {
			t.Errorf("on=%v: small offset got a profile %q", on, got)
		}
		if got := hsp(scene(0.8, "DOME"), learnedProfiles{fakeProfiles{"20": true}, "20"}); got != "https://vr.example/hsp/scene/9" {
			t.Errorf("on=%v: studio profile should win, got %q", on, got)
		}
	}
}

func TestBuildVideoData_LogNeverCarriesApiKey(t *testing.T) {
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", StashApiKey: "secret", FavoriteTag: "FAVORITE",
		LogLevel: "debug", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).Level(zerolog.DebugLevel).WithContext(context.Background())
	// Stash keys the stream urls itself when it uses authentication; the
	// preview is keyed by stash.ApiKeyed.
	sp := &gql.SceneParts{Id: "9", Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Files: []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080, Video_codec: "h264"}},
		Paths: &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream?apikey=secret"), Preview: util.Ptr("http://stash/scene/9/preview")},
		SceneStreams: []*gql.ScenePartsSceneStreamsSceneStreamEndpoint{
			{Url: "http://stash/scene/9/stream?resolution=STANDARD&apikey=secret", Mime_type: util.Ptr("video/mp4"), Label: util.Ptr("MP4 (1080p)")},
		},
	}

	dto, err := buildVideoData(ctx, &library.VideoData{SceneParts: sp}, "https://vr.example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(dto.Media) != 2 || !strings.Contains(dto.Media[0].Sources[0].Url, "apikey=secret") || !strings.Contains(*dto.ThumbnailVideo, "apikey=secret") {
		t.Fatalf("the response itself must stay keyed, got %+v", dto)
	}
	logged := buf.String()
	if !strings.Contains(logged, `"media"`) || !strings.Contains(logged, `"thumbVideo"`) {
		t.Fatalf("expected the media debug line, got %q", logged)
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("api key leaked into the log: %s", logged)
	}
	if strings.Count(logged, "apikey=REDACTED") < 3 {
		t.Fatalf("expected every keyed url redacted in the log, got %s", logged)
	}
}

func TestRedactedMedia_CopiesWithoutTouchingTheOriginal(t *testing.T) {
	in := []mediaDto{{Name: "direct", Sources: []sourceDto{{Resolution: 1080, Url: "http://stash/s?apikey=secret"}}}, {Name: "transcoding"}}

	out := redactedMedia(in)

	if len(out) != 2 || out[0].Name != "direct" || out[0].Sources[0].Resolution != 1080 || out[0].Sources[0].Url != "http://stash/s?apikey=REDACTED" || len(out[1].Sources) != 0 {
		t.Fatalf("unexpected redaction %+v", out)
	}
	if in[0].Sources[0].Url != "http://stash/s?apikey=secret" {
		t.Fatal("the original media must not be changed")
	}
}

func TestBuildVideoData_CarriesSceneDescription(t *testing.T) {
	loadDefaultRules(t)
	scene := func(details *string) *library.VideoData {
		return &library.VideoData{SceneParts: &gql.SceneParts{Id: "9", Details: details, Created_at: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Files: []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
			Paths: &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/9/stream")},
		}}
	}
	cases := []struct {
		name    string
		details *string
		want    string
		inJSON  bool
	}{
		{"details set", util.Ptr("A scene about nine."), "A scene about nine.", true},
		{"details empty", util.Ptr(""), "", false},
		{"details null", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dto, err := buildVideoData(context.Background(), scene(c.details), "https://vr.example", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if dto.Description != c.want {
				t.Fatalf("description %q, want %q", dto.Description, c.want)
			}
			b, _ := json.Marshal(dto)
			if strings.Contains(string(b), `"description"`) != c.inJSON {
				t.Fatalf("description in JSON = %v, want %v: %s", !c.inJSON, c.inJSON, b)
			}
		})
	}
}
