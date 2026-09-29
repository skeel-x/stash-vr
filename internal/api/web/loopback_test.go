package web

import (
	"errors"
	"strings"
	"testing"
)

func TestIsLoopbackHost_Table(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"localhost:9666", true},
		{"LOCALHOST:9666", true},
		{"127.0.0.1", true},
		{"127.0.0.1:9666", true},
		{"127.0.0.2:9666", true}, // the whole 127/8 block loops back
		{"[::1]", true},
		{"[::1]:9666", true},
		{"::1", true},
		{"vr.example", false},
		{"vr.example:9666", false},
		{"10.0.0.2:9666", false},
		{"192.168.1.20", false},
		{"[fe80::1]:9666", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestPlayers_WarnsWhenOpenedViaLoopback(t *testing.T) {
	lib, _ := newEnv(t, &fakeStash{})
	h := PagesRouter(lib)

	for _, host := range []string{"localhost:9666", "127.0.0.1:9666", "[::1]:9666"} {
		body := getPage(t, h, "/", map[string]string{"Host": host}).Body.String()
		if !strings.Contains(body, `id="loopback-note"`) || !strings.Contains(body, "point at this computer only") {
			t.Errorf("%s: expected the loopback note", host)
		}
		// The addresses themselves are still the ones the page was opened with.
		if !strings.Contains(body, `href="http://`+host+`/heresphere"`) {
			t.Errorf("%s: expected the links to keep the host", host)
		}
	}

	body := getPage(t, h, "/", map[string]string{"Host": "10.0.0.2:9666"}).Body.String()
	if strings.Contains(body, `id="loopback-note"`) {
		t.Fatal("a network address must not get the loopback note")
	}

	// With Stash unreachable there are no addresses to warn about.
	lib, _ = newEnv(t, &fakeStash{versionErr: errors.New("down")})
	body = getPage(t, PagesRouter(lib), "/", map[string]string{"Host": "localhost:9666"}).Body.String()
	if strings.Contains(body, `id="loopback-note"`) {
		t.Fatal("the blocked page must not carry the loopback note")
	}
}
