package library

import (
	"testing"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

func tags(names ...string) []*gql.TagPartsArrayTagsTag {
	out := make([]*gql.TagPartsArrayTagsTag, len(names))
	for i, n := range names {
		out[i] = &gql.TagPartsArrayTagsTag{TagParts: gql.TagParts{Id: n, Name: n}}
	}
	return out
}

func TestResolveFormat_DefaultsReproduceLegacyMapping(t *testing.T) {
	rules := config.DefaultVideoRules()
	cases := []struct {
		name string
		tags []*gql.TagPartsArrayTagsTag
		want Format
	}{
		{"dome", tags("DOME"), Format{Projection: "equirectangular", Stereo: "sbs"}},
		{"dome+tb overrides stereo", tags("TB", "DOME"), Format{Projection: "equirectangular", Stereo: "tb"}},
		{"mkx200", tags("MKX200"), Format{Projection: "fisheye", Stereo: "sbs", Lens: "MKX200", Fov: 200}},
		{"rf52", tags("RF52"), Format{Projection: "fisheye", Stereo: "sbs", Fov: 190}},
		{"flat", tags("FLAT"), Format{Projection: "perspective", Stereo: "mono"}},
		{"flat 3d side by side", tags("FLAT", "SBS"), Format{Projection: "perspective", Stereo: "sbs"}},
		{"flat 3d over under", tags("TB", "FLAT"), Format{Projection: "perspective", Stereo: "tb"}},
		{"passthrough", tags("FISHEYE", "Passthrough"), Format{Projection: "fisheye", Stereo: "sbs", Passthrough: true}},
		{"augmented reality", tags("Augmented Reality"), Format{Passthrough: true}},
		{"mkx220", tags("MKX220"), Format{Projection: "fisheye", Stereo: "sbs", Lens: "MKX220", Fov: 220}},
		{"vrca220", tags("VRCA220"), Format{Projection: "fisheye", Stereo: "sbs", Lens: "VRCA220", Fov: 220}},
		{"180 degrees", tags("180°"), Format{Projection: "equirectangular"}},
		{"360 degrees tb", tags("360°", "TB"), Format{Projection: "equirectangular360", Stereo: "tb"}},
		{"mono overrides sbs", tags("SBS", "MONO"), Format{Stereo: "mono"}},
		{"rl swaps eyes only", tags("FISHEYE", "RL"), Format{Projection: "fisheye", Stereo: "sbs", EyeSwap: true, Generated: true}},
		{"unknown", tags("Blonde"), Format{}},
		{"lowercase and alias", []*gql.TagPartsArrayTagsTag{{TagParts: gql.TagParts{Id: "1", Name: "Half dome", Aliases: []string{"dome"}}}}, Format{Projection: "equirectangular", Stereo: "sbs"}},
	}
	for _, c := range cases {
		if got := ResolveFormat(rules, c.tags); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestResolveFormat_LaterRulesOverrideAndProfileCarries(t *testing.T) {
	rules := []config.VideoRule{
		{Tag: "A", Projection: "fisheye", Fov: 190, Passthrough: true},
		{Tag: "B", Projection: "equirectangular", Profile: "42"},
		{Tag: "C", Passthrough: false, Fov: 0},
	}
	got := ResolveFormat(rules, tags("C", "B", "A"))
	want := Format{Projection: "equirectangular", Fov: 190, Passthrough: true, ProfileScene: "42"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got := ResolveFormat(nil, tags("A")); got != (Format{}) {
		t.Fatalf("no rules must give an empty format, got %+v", got)
	}
}

func fp(v float64) *float64 { return &v }

func TestResolveFormat_GeometryPerFieldAndGenerated(t *testing.T) {
	rules := []config.VideoRule{
		{Tag: "A", PositionX: fp(1), PositionY: fp(2), ZoomX: fp(1.5), Background: "color", BackgroundColor: "#ff0000"},
		{Tag: "B", PositionY: fp(4.78), Yaw: fp(-3), OriginZ: fp(0.05), Mask: "alpha"},
		{Tag: "C", Background: "passthrough"},
	}
	got := ResolveFormat(rules, tags("A", "B", "C"))
	if *got.PositionX != 1 || *got.PositionY != 4.78 || *got.ZoomX != 1.5 || *got.Yaw != -3 || *got.OriginZ != 0.05 {
		t.Fatalf("geometry %+v", got)
	}
	if got.PositionZ != nil || got.Pitch != nil || got.PanX != nil || got.OriginX != nil {
		t.Fatalf("unset fields must stay nil: %+v", got)
	}
	if got.Background != "passthrough" || got.BackgroundColor != "#ff0000" || got.Mask != "alpha" || !got.Generated {
		t.Fatalf("environment %+v", got)
	}

	for _, r := range []config.VideoRule{
		{Tag: "A", PanY: fp(0)},
		{Tag: "A", Roll: fp(0)},
		{Tag: "A", Mask: "none"},
		{Tag: "A", Background: "global"},
		{Tag: "A", BackgroundColor: "#000000"},
	} {
		if f := ResolveFormat([]config.VideoRule{r}, tags("A")); !f.Generated {
			t.Errorf("rule %+v must mark the format generated", r)
		}
	}
	for _, r := range []config.VideoRule{
		{Tag: "A", Passthrough: true, Projection: "fisheye", Fov: 190, Lens: "MKX200", Profile: "42"},
		{Tag: "B", PositionX: fp(1)},
	} {
		if f := ResolveFormat([]config.VideoRule{r}, tags("A")); f.Generated {
			t.Errorf("rule %+v must not mark the format generated", r)
		}
	}
}

func TestResolveFormat_EyeSwapAndForceMono(t *testing.T) {
	yes, no := true, false
	rules := []config.VideoRule{
		{Tag: "RL", EyeSwap: &yes},
		{Tag: "MONO", Stereo: "mono"},
		{Tag: "FM", ForceMono: &yes},
		{Tag: "NOSWAP", EyeSwap: &no},
	}
	if got := ResolveFormat(rules, tags("RL")); !got.EyeSwap || got.ForceMono || !got.Generated || got.Stereo != "" {
		t.Fatalf("RL: %+v", got)
	}
	if got := ResolveFormat(rules, tags("MONO")); got.EyeSwap || got.ForceMono || got.Generated || got.Stereo != "mono" {
		t.Fatalf("MONO: %+v", got)
	}
	if got := ResolveFormat(rules, tags("FM")); !got.ForceMono || !got.Generated {
		t.Fatalf("FM: %+v", got)
	}
	if got := ResolveFormat(rules, tags("RL", "NOSWAP")); got.EyeSwap || got.Generated {
		t.Fatalf("a later rule turning eye swap off must win: %+v", got)
	}
}
