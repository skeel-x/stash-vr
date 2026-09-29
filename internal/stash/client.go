package stash

import (
	"context"
	"crypto/tls"
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

func NewClient(graphqlUrl string, apiKey string) graphql.Client {
	defaultTr, _ := http.DefaultTransport.(*http.Transport)
	transport := defaultTr.Clone()
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	transport.ResponseHeaderTimeout = responseHeaderTimeout

	var rt http.RoundTripper = transport
	if apiKey != "" {
		rt = &authTransport{
			apiKey: apiKey,
			rt:     transport,
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
