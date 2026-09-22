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
