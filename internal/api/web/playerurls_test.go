package web

import "testing"

func TestStashSceneUrl(t *testing.T) {
	cases := map[string]string{
		"https://stash.example/graphql":       "https://stash.example/scenes/42",
		"http://stash:9999/graphql":           "http://stash:9999/scenes/42",
		"https://stash.example/stash/graphql": "https://stash.example/stash/scenes/42",
		"https://stash.example":               "https://stash.example/scenes/42",
		"https://stash.example/":              "https://stash.example/scenes/42",
	}
	for in, want := range cases {
		if got := StashSceneUrl(in, "42"); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}
