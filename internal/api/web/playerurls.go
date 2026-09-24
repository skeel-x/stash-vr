package web

import (
	"net/http"
	"strings"

	"stash-vr/internal/api/internal"
)

// PlayerLinks are the URLs a user types into each player, derived from the
// host and scheme the page was opened with.
type PlayerLinks struct {
	Base       string `json:"base"`
	HereSphere string `json:"heresphere"`
	DeoVR      string `json:"deovr"`
	DeoVRApi   string `json:"deovr_api"`
	Playa      string `json:"playa"`
	PlainHTTP  bool   `json:"plain_http"`
}

// StashSceneUrl turns the configured GraphQL endpoint into the Stash web
// UI page for a scene.
func StashSceneUrl(graphqlUrl, id string) string {
	return stashWebBase(graphqlUrl) + "/scenes/" + id
}

// stashWebBase is the Stash web UI root for the configured GraphQL
// endpoint.
func stashWebBase(graphqlUrl string) string {
	return strings.TrimSuffix(strings.TrimSuffix(graphqlUrl, "/graphql"), "/")
}

func LinksFor(req *http.Request) PlayerLinks {
	base := internal.GetBaseUrl(req)
	return PlayerLinks{
		Base:       base,
		HereSphere: base + "/heresphere",
		DeoVR:      base,
		DeoVRApi:   base + "/deovr",
		Playa:      base,
		PlainHTTP:  strings.HasPrefix(base, "http://"),
	}
}
