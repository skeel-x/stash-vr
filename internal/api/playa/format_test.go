package playa

import (
	"testing"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
)

func loadDefaultRules(t *testing.T) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", FavoriteTag: "FAVORITE",
		LogLevel: "info", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionAndStereo_FromRules(t *testing.T) {
	loadDefaultRules(t)
	cases := []struct {
		tags               []string
		projection, stereo string
	}{
		{[]string{"DOME"}, "180", "LR"},
		{[]string{"SPHERE", "TB"}, "360", "TB"},
		{[]string{"MKX200"}, "FSH", "LR"},
		{[]string{"RF52"}, "FSH", "LR"},
		{[]string{"FLAT"}, "FLT", "MN"},
		{[]string{"SBS"}, "180", "LR"},
		{[]string{"Virtual Reality"}, "180", "LR"},
		{[]string{"Blonde"}, "FLT", "MN"},
	}
	for _, c := range cases {
		sp := &gql.SceneParts{Id: "1"}
		for i, n := range c.tags {
			sp.Tags = append(sp.Tags, &gql.TagPartsArrayTagsTag{TagParts: gql.TagParts{Id: string(rune('a' + i)), Name: n}})
		}
		p, s := projectionAndStereo(&library.VideoData{SceneParts: sp})
		if p != c.projection || s != c.stereo {
			t.Errorf("%v: got %s/%s want %s/%s", c.tags, p, s, c.projection, c.stereo)
		}
	}
}
