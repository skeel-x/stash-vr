package stash

import "testing"

func TestRedacted(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"keyed", "http://stash:9999/scene/1/screenshot?apikey=secret", "http://stash:9999/scene/1/screenshot?apikey=REDACTED"},
		{"keyed with other params", "http://stash:9999/x?a=b&apikey=secret", "http://stash:9999/x?a=b&apikey=REDACTED"},
		{"unkeyed", "http://stash:9999/scene/1/screenshot", "http://stash:9999/scene/1/screenshot"},
		{"signed", "http://stash:9999/scene/1/stream?cid=abc&expires=1794277850&signature=deadbeef", "http://stash:9999/scene/1/stream?cid=REDACTED&expires=1794277850&signature=REDACTED"},
		{"signed and keyed", "http://stash:9999/scene/1/stream?cid=abc&signature=deadbeef&apikey=secret", "http://stash:9999/scene/1/stream?apikey=REDACTED&cid=REDACTED&signature=REDACTED"},
		{"invalid", "http://[::1", "http://[::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Redacted(c.in)
			if got != c.want {
				t.Errorf("Redacted(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
