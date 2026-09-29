package stash

import (
	"context"
	"github.com/Khan/genqlient/graphql"
	"net/http"
	"stash-vr/internal/stash/gql"
	"time"
)

// responseHeaderTimeout bounds how long a request waits for Stash to start
// answering. A Stash that accepts the connection and then goes silent (a
// wedged database, say) would otherwise leave the request, and everything
// waiting on it, hanging until the process is restarted. Long enough for a
// scene query over a large library. A variable so tests can shrink it.
var responseHeaderTimeout = 3 * time.Minute

type authTransport struct {
	apiKey string
	rt     http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.Header.Add("ApiKey", t.apiKey)
	return t.rt.RoundTrip(req2)
}

// NewClient returns a GraphQL client for Stash at graphqlUrl on the shared
// transport (see Transport). tlsInsecure skips verifying Stash's
// certificate, for self-signed https.
func NewClient(graphqlUrl string, apiKey string, tlsInsecure bool) graphql.Client {
	var rt http.RoundTripper = Transport(tlsInsecure)
	if apiKey != "" {
		rt = &authTransport{
			apiKey: apiKey,
			rt:     rt,
		}
	}

	htc := &http.Client{
		Transport: rt,
	}

	return graphql.NewClient(graphqlUrl, htc)
}

func GetVersion(ctx context.Context, client graphql.Client) (string, error) {
	version, err := gql.Version(ctx, client)
	if err != nil {
		return "", err
	}
	return *version.Version.Version, nil
}
