package internal

import (
	"net/http/httptest"
	"testing"

	"stash-vr/internal/config"
)

func TestGetBaseUrl_PrefixFromHeaderThenSetting(t *testing.T) {
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", FavoriteTag: "FAVORITE",
		LogLevel: "info", ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(), BasePath: "/fromsetting",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "vr.example"
	if got := GetBaseUrl(req); got != "http://vr.example/fromsetting" {
		t.Fatalf("setting prefix: got %q", got)
	}

	req.Header.Set("X-Forwarded-Prefix", "/stashvr/")
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := GetBaseUrl(req); got != "https://vr.example/stashvr" {
		t.Fatalf("header prefix: got %q", got)
	}
	if got := GetBasePath(req); got != "/stashvr" {
		t.Fatalf("GetBasePath: got %q", got)
	}
}
