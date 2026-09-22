package internal

import (
	"net/http"
	"strings"

	"stash-vr/internal/config"
	"stash-vr/internal/util"
)

// GetBasePath returns the path prefix stash-vr is served under: the
// X-Forwarded-Prefix header from a reverse proxy, else the base_path setting.
// Always "" or "/name" with no trailing slash.
func GetBasePath(req *http.Request) string {
	if p := strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")); p != "" {
		if n, err := config.NormalizeBasePath(p); err == nil {
			return n
		}
	}
	return config.Application().BasePath
}

func GetBaseUrl(req *http.Request) string {
	return util.GetScheme(req) + "://" + req.Host + GetBasePath(req)
}
