package stash

import (
	"net/url"
	"stash-vr/internal/config"
	"strings"
)

func ApiKeyed(url string) string {
	apiKey := config.Application().StashApiKey
	if apiKey == "" || strings.Contains(url, "apikey") {
		return url
	}
	if strings.Contains(url, "?") {
		return url + "&apikey=" + apiKey
	}

	return url + "?apikey=" + apiKey
}

// Redacted returns u with any apikey query value replaced by REDACTED, for logs.
func Redacted(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	if q.Has("apikey") {
		q.Set("apikey", "REDACTED")
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}
