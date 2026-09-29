package heatmap

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stash-vr/internal/config"
)

// selfSignedStash serves a JPEG screenshot behind a self-signed certificate.
func selfSignedStash(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 90, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The cover fetcher shares the GraphQL client's transport, so a self-signed
// https Stash serves covers exactly when the TLS setting allows it.
func TestFetchScreenshot_FollowsTLSSetting(t *testing.T) {
	srv := selfSignedStash(t)
	cases := []struct {
		name     string
		insecure bool
		wantErr  string
	}{
		{name: "verified by default", wantErr: "certificate"},
		{name: "insecure setting accepts self-signed", insecure: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := config.Load(config.ApplicationConfig{
				ListenAddress: ":9666", StashGraphQLUrl: srv.URL + "/graphql", StashTLSInsecure: c.insecure, LogLevel: "info",
				ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
			}); err != nil {
				t.Fatal(err)
			}

			ct, body, err := fetchBytes(context.Background(), srv.URL+"/scene/1/screenshot")

			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ct != "image/jpeg" || len(body) == 0 {
				t.Fatalf("expected the screenshot back, got %q with %d bytes", ct, len(body))
			}
		})
	}
}
