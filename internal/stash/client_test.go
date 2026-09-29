package stash

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	cases := []struct {
		name    string
		apiKey  string
		silent  bool // accept the request and never answer
		wantErr string
		wantKey string
	}{
		{name: "answers with the api key sent", apiKey: "secret", wantKey: "secret"},
		{name: "answers without an api key"},
		{name: "silent stash times out", silent: true, wantErr: "timeout awaiting response headers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prev := responseHeaderTimeout
			responseHeaderTimeout = 100 * time.Millisecond
			t.Cleanup(func() { responseHeaderTimeout = prev })

			var mu sync.Mutex
			var gotKey string
			// release lets a silent handler return once the client gave up.
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				gotKey = r.Header.Get("ApiKey")
				mu.Unlock()
				if c.silent {
					// Hold the response back until the client gave up.
					<-release
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"version":{"version":"v0.1.0"}}}`))
			}))
			// Cleanups run last registered first: the handler is released
			// before the server waits for it.
			t.Cleanup(srv.Close)
			t.Cleanup(func() { close(release) })

			client := NewClient(srv.URL, c.apiKey)
			start := time.Now()
			version, err := GetVersion(context.Background(), client)
			elapsed := time.Since(start)

			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
				}
				if elapsed > 5*time.Second {
					t.Fatalf("the silent request must be given up on promptly, took %s", elapsed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if version != "v0.1.0" {
				t.Fatalf("expected the version back, got %q", version)
			}
			mu.Lock()
			defer mu.Unlock()
			if gotKey != c.wantKey {
				t.Fatalf("expected ApiKey header %q, got %q", c.wantKey, gotKey)
			}
		})
	}
}
