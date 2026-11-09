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

// WithQuery appends the query string q (without a leading "?") to u, using
// "&" when u already carries a query. Stash 0.32 signs the caption path, so
// it arrives with cid, expires and signature already set.
func WithQuery(u, q string) string {
	if q == "" {
		return u
	}
	if strings.Contains(u, "?") {
		return u + "&" + q
	}
	return u + "?" + q
}

// Redacted returns u with the apikey, cid and signature query values
// replaced by REDACTED, for logs.
func Redacted(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	changed := false
	// apikey is Stash's API key; cid and signature are the credential id
	// and HMAC of the signed urls Stash hands out since 0.32, which grant
	// access on their own until they expire.
	for _, k := range []string{"apikey", "cid", "signature"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}
