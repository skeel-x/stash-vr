package heresphere

import (
	"net/http/httptest"
	"testing"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/hsp"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func profileScene() *library.VideoData {
	return &library.VideoData{SceneParts: &gql.SceneParts{
		Id: "31", Title: util.Ptr("Thirty One"), Date: util.Ptr("2023-05-06"), Rating100: util.Ptr(80),
		Created_at:    time.Date(2024, 2, 3, 15, 4, 5, 0, time.UTC),
		Files:         []*gql.ScenePartsFilesVideoFile{{Basename: "x.mp4", Duration: 120}},
		Paths:         &gql.ScenePartsPathsScenePathsType{Stream: util.Ptr("http://stash/scene/31/stream")},
		TagPartsArray: gql.TagPartsArray{Tags: []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: "Passthrough"}}, {TagParts: gql.TagParts{Id: "2", Name: "FISHEYE"}}}},
	}}
}

func TestBuildProfile_CarriesRuleGeometryAndScene(t *testing.T) {
	loadDefaultRules(t)
	f := func(v float64) *float64 { return &v }
	format := library.Format{
		Projection: "fisheye", Stereo: "tb", Lens: "MKX200", Fov: 200, Passthrough: true,
		PositionX: f(0.5), PositionY: f(4.78), PositionZ: f(-1), Pitch: f(10), Yaw: f(-20), Roll: f(3),
		ZoomX: f(1.25), ZoomY: f(0.75), PanX: f(0.1), PanY: f(-0.2), OriginX: f(-0.06), OriginY: f(-0.02), OriginZ: f(0.05),
		Generated: true,
	}
	vd := profileScene()
	data, err := generateProfile(vd, "https://vr.example", format)
	if err != nil {
		t.Fatal(err)
	}
	p, err := hsp.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "https://vr.example/heresphere/31" || p.Title != "Thirty One" {
		t.Fatalf("scene fields %q %q", p.ID, p.Title)
	}
	if p.Duration != hsp.TimespanTicks(120) {
		t.Fatalf("duration %d", p.Duration)
	}
	if p.DateReleased != hsp.DateTicks(time.Date(2023, 5, 6, 0, 0, 0, 0, time.UTC)) || p.DateAdded != hsp.DateTicks(time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dates %d %d", p.DateReleased, p.DateAdded)
	}
	if p.AverageRating != 4 {
		t.Fatalf("rating %v", p.AverageRating)
	}
	want := getTags(vd)
	if len(p.Tags) != len(want) || len(want) == 0 {
		t.Fatalf("got %d tags, want %d", len(p.Tags), len(want))
	}
	for i, tag := range want {
		if p.Tags[i].Name != tag.Name {
			t.Fatalf("tag %d: %q want %q", i, p.Tags[i].Name, tag.Name)
		}
		if tag.Track != nil && p.Tags[i].Track != uint32(*tag.Track) {
			t.Fatalf("tag %d track %d want %d", i, p.Tags[i].Track, *tag.Track)
		}
		if tag.End == nil && p.Tags[i].End != p.Duration {
			t.Fatalf("tag %d without end must run to the end, got %d", i, p.Tags[i].End)
		}
	}
	fk := p.Format[0]
	if fk.Projection != hsp.ProjectionFisheye || fk.Stereo != hsp.StereoTopBottom || fk.Zoom != (hsp.Vec2{X: 1.25, Y: 0.75}) || fk.Pan != (hsp.Vec2{X: 0.1, Y: -0.2}) {
		t.Fatalf("format %+v", fk)
	}
	if l := p.Lens[0]; l.TrueLens != "MKX200" || l.ExportLens != "MKX200" || l.TrueFOV != 200 || l.ExportFOV != 200 {
		t.Fatalf("lens %+v", l)
	}
	if a := p.Alignment[0]; a.Position != (hsp.Vec3{X: 0.5, Y: 4.78, Z: -1}) || a.Rotation != (hsp.Rotator{Pitch: 10, Yaw: -20, Roll: 3}) {
		t.Fatalf("alignment %+v", a)
	}
	if o := p.Origin[0].Origin; o != (hsp.Vec3{X: -0.06, Y: -0.02, Z: 0.05}) {
		t.Fatalf("origin %+v", o)
	}
	if e := p.Environment[0]; e.Background != hsp.BackgroundPassthrough || e.Mask != hsp.MaskAlphaPacked {
		t.Fatalf("passthrough rule must give passthrough background and alpha mask, got %d/%d", e.Background, e.Mask)
	}
}

func TestBuildProfile_Defaults(t *testing.T) {
	loadDefaultRules(t)
	vd := profileScene()
	vd.SceneParts.Date = nil
	p := buildProfile(vd, "https://vr.example", library.Format{Generated: true})
	d := hsp.Default()
	if p.Format[0] != d.Format[0] || p.Lens[0] != d.Lens[0] || p.Alignment[0] != d.Alignment[0] || p.Environment[0] != d.Environment[0] {
		t.Fatalf("an empty format must keep the defaults: %+v", p)
	}
	if p.DateReleased != 0 {
		t.Fatalf("no release date must be 0, got %d", p.DateReleased)
	}
}

func TestBuildProfile_EnvironmentFields(t *testing.T) {
	loadDefaultRules(t)
	cases := []struct {
		format     library.Format
		background uint8
		mask       uint8
	}{
		{library.Format{Background: "color", BackgroundColor: "#ffffff", Mask: "chroma"}, hsp.BackgroundColor, hsp.MaskChromaKey},
		{library.Format{Background: "global", Mask: "none", Passthrough: true}, hsp.BackgroundGlobal, hsp.MaskNone},
		{library.Format{Background: "passthrough"}, hsp.BackgroundPassthrough, hsp.MaskNone},
		{library.Format{Mask: "alpha"}, hsp.BackgroundGlobal, hsp.MaskAlphaPacked},
		{library.Format{Passthrough: true, Mask: "none"}, hsp.BackgroundPassthrough, hsp.MaskNone},
	}
	for _, c := range cases {
		e := buildProfile(profileScene(), "https://vr.example", c.format).Environment[0]
		if e.Background != c.background || e.Mask != c.mask {
			t.Errorf("%+v: got %d/%d want %d/%d", c.format, e.Background, e.Mask, c.background, c.mask)
		}
	}
	e := buildProfile(profileScene(), "https://vr.example", cases[0].format).Environment[0]
	if e.BackgroundColor != (hsp.Color{R: 1, G: 1, B: 1, A: 1}) {
		t.Fatalf("colour %+v", e.BackgroundColor)
	}
}

func TestBuildProfile_Projections(t *testing.T) {
	loadDefaultRules(t)
	for name, want := range map[string]uint8{
		"perspective": hsp.ProjectionPerspective, "equirectangular": hsp.ProjectionEquirectangular,
		"equirectangular360": hsp.ProjectionEquirectangular360, "fisheye": hsp.ProjectionFisheye,
		"cubemap": hsp.ProjectionCubemap, "equiangularCubemap": hsp.ProjectionEquiangularCubemap,
	} {
		if got := buildProfile(profileScene(), "", library.Format{Projection: name}).Format[0].Projection; got != want {
			t.Errorf("%s: %d want %d", name, got, want)
		}
	}
	for name, want := range map[string]uint8{"mono": hsp.StereoMono, "sbs": hsp.StereoSideBySide, "tb": hsp.StereoTopBottom} {
		if got := buildProfile(profileScene(), "", library.Format{Stereo: name}).Format[0].Stereo; got != want {
			t.Errorf("%s: %d want %d", name, got, want)
		}
	}
}

func TestGenerateProfile_UsesRequestBaseUrlAndRejectsSceneWithoutFiles(t *testing.T) {
	loadDefaultRules(t)
	cfg := config.Application()
	cfg.BasePath = "/vr"
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://stash-vr.lan/hsp/scene/31", nil)
	data, err := GenerateProfile(req, profileScene(), library.Format{Generated: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := hsp.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "http://stash-vr.lan/vr/heresphere/31" {
		t.Fatalf("id %q", p.ID)
	}
	vd := profileScene()
	vd.SceneParts.Files = nil
	if _, err := GenerateProfile(req, vd, library.Format{Generated: true}); err == nil {
		t.Fatal("expected an error for a scene without files")
	}
}

func TestBuildVideoData_LinksGeneratedProfile(t *testing.T) {
	loadDefaultRules(t)
	cfg := config.Application()
	y := 4.78
	cfg.VideoRules = append(cfg.VideoRules, config.VideoRule{Tag: "Passthrough", PositionY: &y})
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	dto, err := buildVideoData(t.Context(), profileScene(), "https://vr.example", nil, fakeProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Hsp == nil || *dto.Hsp != "https://vr.example/hsp/scene/31" {
		t.Fatalf("hsp = %v", dto.Hsp)
	}
}
