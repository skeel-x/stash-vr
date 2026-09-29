package stash

import (
	"crypto/tls"
	"net/http"
	"stash-vr/internal/config"
	"sync"
	"time"
)

// Stash's TLS certificate is verified by default. Earlier releases skipped
// verification on every request, so an https Stash with a self-signed
// certificate that used to work out of the box now needs STASH_TLS_INSECURE
// (the "Skip TLS certificate verification" switch on the Setup page). This
// is a deliberate behaviour change: a certificate that is never checked
// protects nothing.

var (
	transportMu sync.Mutex
	// transports holds the shared transport per TLS mode, built on first use.
	transports [2]*http.Transport
)

// Transport returns the http.Transport shared by the GraphQL client and the
// cover, heatmap and poster fetchers for the given TLS mode, so every
// request to Stash treats the certificate the same way and connections are
// pooled across callers. One transport is kept per mode.
func Transport(insecure bool) *http.Transport {
	transportMu.Lock()
	defer transportMu.Unlock()
	i := 0
	if insecure {
		i = 1
	}
	if transports[i] == nil {
		transports[i] = newTransport(insecure)
	}
	return transports[i]
}

func newTransport(insecure bool) *http.Transport {
	defaultTr, _ := http.DefaultTransport.(*http.Transport)
	tr := defaultTr.Clone()
	tr.TLSClientConfig = &tls.Config{
		// Opt-in only, for self-signed certificates; see the note above.
		InsecureSkipVerify: insecure,
	}
	tr.ResponseHeaderTimeout = responseHeaderTimeout
	return tr
}

// resetTransports drops the shared transports so the next call builds them
// afresh; tests use it after changing responseHeaderTimeout.
func resetTransports() {
	transportMu.Lock()
	defer transportMu.Unlock()
	for i, tr := range transports {
		if tr != nil {
			tr.CloseIdleConnections()
		}
		transports[i] = nil
	}
}

// settingTransport sends each request through the shared transport for the
// TLS mode in the current settings, so a fetcher follows a change made on
// the Setup page without being rebuilt.
type settingTransport struct{}

func (settingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return Transport(config.Application().StashTLSInsecure).RoundTrip(req)
}

// HTTPClient returns a client for fetching files (covers, heatmaps,
// posters) from Stash on the shared transport, with timeout bounding each
// request as a whole.
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: settingTransport{}}
}
