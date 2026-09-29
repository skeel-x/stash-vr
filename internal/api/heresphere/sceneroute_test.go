package heresphere

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"stash-vr/internal/library"
)

// lockedBuffer is a bytes.Buffer safe to log to from several goroutines:
// the handler's background work logs on the request logger.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// sceneRequest builds a scene request for id with the raw body (nil for
// none) and a logger writing to buf on its context.
func sceneRequest(method, id string, body []byte, buf *lockedBuffer) *http.Request {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, "/"+id, http.NoBody)
	} else {
		req = httptest.NewRequest(method, "/"+id, bytes.NewReader(body))
	}
	ctx := zerolog.New(buf).Level(zerolog.DebugLevel).WithContext(context.Background())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, &chi.Context{URLParams: chi.RouteParams{Keys: []string{"videoId"}, Values: []string{id}}})
	return req.WithContext(ctx)
}

func TestVideoData_BodyHandling(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		body       []byte
		wantLevel  string
		wantMsg    string
		notLogged  string
		wantUpdate bool
	}{
		{
			name:      "browser GET without a body is served at debug",
			method:    http.MethodGet,
			body:      nil,
			wantLevel: `"level":"debug"`,
			wantMsg:   "without a body",
			notLogged: `"level":"warn"`,
		},
		{
			name:      "POST with an empty body is served at debug",
			method:    http.MethodPost,
			body:      nil,
			wantLevel: `"level":"debug"`,
			wantMsg:   "without a body",
			notLogged: `"level":"warn"`,
		},
		{
			name:      "malformed body still warns",
			method:    http.MethodPost,
			body:      []byte(`{"rating":`),
			wantLevel: `"level":"warn"`,
			wantMsg:   "Failed to parse request body",
		},
		{
			name:       "well formed body runs the updates",
			method:     http.MethodPost,
			body:       []byte(`{"rating":4.5}`),
			notLogged:  "Failed to parse request body",
			wantUpdate: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loadDefaultRules(t)
			stash := &explodingStash{}
			h := newHttpHandler(library.NewService(stash))
			var buf lockedBuffer
			rec := httptest.NewRecorder()

			h.videoDataHandler(rec, sceneRequest(c.method, "7", c.body, &buf))
			if c.wantUpdate {
				waitFor(t, func() bool { return stash.sawOp("SceneUpdateRating100") })
			}
			time.Sleep(50 * time.Millisecond)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected the scene document, got %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), `"title":"Seven"`) {
				t.Fatalf("expected the scene document, got %s", rec.Body.String())
			}
			out := buf.String()
			if c.wantLevel != "" && (!strings.Contains(out, c.wantLevel) || !strings.Contains(out, c.wantMsg)) {
				t.Fatalf("expected %s %q in the log, got %q", c.wantLevel, c.wantMsg, out)
			}
			if c.notLogged != "" && strings.Contains(out, c.notLogged) {
				t.Fatalf("log must not carry %q, got %q", c.notLogged, out)
			}
			if got := stash.sawOp("SceneUpdateRating100"); got != c.wantUpdate {
				t.Fatalf("update ran = %v, want %v", got, c.wantUpdate)
			}
		})
	}
}

func TestLoadingPage_LinksHonourBasePath(t *testing.T) {
	loadDefaultRules(t)
	r := Router(library.NewService(&explodingStash{}))
	cases := []struct {
		name   string
		prefix string
		base   string
	}{
		{name: "at the root", prefix: "", base: ""},
		{name: "behind a sub-path", prefix: "/vr", base: "/vr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			if c.prefix != "" {
				req.Header.Set("X-Forwarded-Prefix", c.prefix)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("expected an html page, got %q", ct)
			}
			body := rec.Body.String()
			for _, want := range []string{
				`href="` + c.base + `/"`,
				`src="` + c.base + `/icon.png"`,
				`href="` + c.base + `/icon.png"`,
				"only this page",
				"HereSphere",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("page lacks %q:\n%s", want, body)
				}
			}
			if strings.Contains(body, "{{") {
				t.Fatalf("page has an unrendered template action:\n%s", body)
			}
		})
	}
}

func TestLoadingPage_PostStillServesTheIndex(t *testing.T) {
	loadDefaultRules(t)
	r := Router(library.NewService(&explodingStash{}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected the JSON index for HereSphere's POST, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
