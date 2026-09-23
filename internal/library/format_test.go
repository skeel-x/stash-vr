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
		{"passthrough", tags("FISHEYE", "Passthrough"), Format{Projection: "fisheye", Stereo: "sbs", Passthrough: true}},
		{"augmented reality", tags("Augmented Reality"), Format{Passthrough: true}},
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
