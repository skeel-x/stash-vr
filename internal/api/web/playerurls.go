package web

import (
	"net"
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
	// Loopback is set when the page was opened via localhost, 127.0.0.1 or
	// [::1]: the addresses then only work on this computer, never in a
	// headset. The page says so; it does not go looking for a better
	// address, which would mean enumerating the network interfaces.
	Loopback bool `json:"loopback"`
}

// isLoopbackHost reports whether host (with or without a port) names this
// computer only: localhost or a loopback IP such as 127.0.0.1 or ::1.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
		Loopback:   isLoopbackHost(req.Host),
	}
}
