package heresphere

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"
	"stash-vr/internal/library"
)

// explodingStash serves scene 7 and panics on the operations listed in
// panicOn, recording which operations it saw.
type explodingStash struct {
	mu      sync.Mutex
	panicOn map[string]bool
	seen    []string
	// ctxs records, per operation seen, the state of the context it was
	// made with, for the tests that check the detached background work.
	ctxs    []ctxState
	noScene bool
}

// ctxState is what a fake observed of one request's context.
type ctxState struct {
	op          string
	err         error
	hasDeadline bool
}

func (e *explodingStash) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	_, hasDeadline := ctx.Deadline()
	e.mu.Lock()
	e.seen = append(e.seen, req.OpName)
	e.ctxs = append(e.ctxs, ctxState{op: req.OpName, err: ctx.Err(), hasDeadline: hasDeadline})
	boom := e.panicOn[req.OpName]
	e.mu.Unlock()
	if boom {
		panic("stash exploded on " + req.OpName)
	}
	var payload string
	switch req.OpName {
	case "FindScenes":
		if e.noScene {
			payload = `{"findScenes":{"scenes":[]}}`
		} else {
			payload = `{"findScenes":{"scenes":[{"id":"7","title":"Seven","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"seven.mp4","duration":1000,"path":"/seven.mp4","height":1080,"video_codec":"h264"}],"paths":{"stream":"http://stash/scene/7/stream"},"tags":[]}]}}`
		}
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindAllSceneIds", "FindSceneIdsByFilter":
		payload = `{"findScenes":{"scenes":[{"id":"7"}]}}`
	case "UIConfiguration":
		payload = `{"configuration":{"ui":{}}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

// ctxOf returns the context state of the first request for op.
func (e *explodingStash) ctxOf(op string) (ctxState, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.ctxs {
		if c.op == op {
			return c, true
		}
	}
	return ctxState{}, false
}

func (e *explodingStash) sawOp(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.seen {
		if s == name {
			return true
		}
	}
	return false
}

func videoDataRequest(t *testing.T, id string, body map[string]any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/"+id, bytes.NewReader(b))
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{URLParams: chi.RouteParams{Keys: []string{"videoId"}, Values: []string{id}}}))
}

func TestVideoData_UnknownSceneSkipsUpdates(t *testing.T) {
	loadDefaultRules(t)
	stash := &explodingStash{noScene: true}
	h := &httpHandler{libraryService: library.NewService(stash)}
	rec := httptest.NewRecorder()

	h.videoDataHandler(rec, videoDataRequest(t, "999999", map[string]any{"rating": 4.5}))
	time.Sleep(50 * time.Millisecond)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if stash.sawOp("SceneUpdateRating100") {
		t.Fatal("updates must not run for a scene that does not exist")
	}
}

func TestVideoData_PanicInUpdatesIsRecovered(t *testing.T) {
	loadDefaultRules(t)
	stash := &explodingStash{panicOn: map[string]bool{"SceneUpdateRating100": true}}
	h := &httpHandler{libraryService: library.NewService(stash)}
	rec := httptest.NewRecorder()

	h.videoDataHandler(rec, videoDataRequest(t, "7", map[string]any{"rating": 4.5}))
	waitFor(t, func() bool { return stash.sawOp("SceneUpdateRating100") })
	time.Sleep(50 * time.Millisecond)

	if rec.Code != http.StatusOK {
		t.Fatalf("the scene document must still be served, got %d", rec.Code)
	}
	// The process is still here: a second request works as before.
	rec = httptest.NewRecorder()
	h.videoDataHandler(rec, videoDataRequest(t, "7", map[string]any{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after the recovered panic, got %d", rec.Code)
	}
}

func TestIndex_PanicInScenePrefetchIsRecovered(t *testing.T) {
	loadDefaultRules(t)
	stash := &explodingStash{panicOn: map[string]bool{"FindScenes": true}}
	h := &httpHandler{libraryService: library.NewService(stash)}
	rec := httptest.NewRecorder()

	h.indexHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	waitFor(t, func() bool { return stash.sawOp("FindScenes") })
	time.Sleep(50 * time.Millisecond)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
