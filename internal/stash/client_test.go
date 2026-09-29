package stash

import (
	"context"
	"errors"
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
			// The shared transports carry the timeout they were built with.
			resetTransports()
			t.Cleanup(func() { responseHeaderTimeout = prev; resetTransports() })

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

			client := NewClient(srv.URL, c.apiKey, false)
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

// versionTLSServer is a Stash behind a self-signed certificate.
func versionTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"version":{"version":"v0.1.0"}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewClient_VerifiesCertificateUnlessInsecure(t *testing.T) {
	srv := versionTLSServer(t)
	cases := []struct {
		name     string
		insecure bool
		wantErr  string
	}{
		// Verification is on by default: a self-signed Stash is refused.
		{name: "verified by default", wantErr: "certificate"},
		{name: "insecure accepts self-signed", insecure: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			version, err := GetVersion(context.Background(), NewClient(srv.URL, "", c.insecure))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil || version != "v0.1.0" {
				t.Fatalf("expected the version back, got %q, %v", version, err)
			}
		})
	}
}

func TestTransport_SharedPerMode(t *testing.T) {
	resetTransports()
	t.Cleanup(resetTransports)
	verified, insecure := Transport(false), Transport(true)
	if Transport(false) != verified || Transport(true) != insecure {
		t.Fatal("expected one transport per mode")
	}
	if verified == insecure {
		t.Fatal("expected the verified and insecure transports to differ")
	}
	if verified.TLSClientConfig.InsecureSkipVerify || !insecure.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify to follow the mode")
	}
}

func TestGetVersion_AnswerWithoutAVersionIsAnError(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "null version object", body: `{"data":{"version":null}}`},
		{name: "null version string", body: `{"data":{"version":{"version":null}}}`},
		{name: "empty data", body: `{"data":{}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(c.body))
			}))
			t.Cleanup(srv.Close)

			version, err := GetVersion(context.Background(), NewClient(srv.URL, "", false))
			if !errors.Is(err, errNoVersion) {
				t.Fatalf("expected errNoVersion, got %q %v", version, err)
			}
		})
	}
}
