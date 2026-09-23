package heresphere

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"
	"stash-vr/internal/library"
)

// fakeStash answers the scene lookup and records every activity save: each
// resume position saved (skipping saves that carried none), and how many
// saves also carried played seconds.
type fakeStash struct {
	mu          sync.Mutex
	resumes     []float64
	withSeconds int
}

func (f *fakeStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindScenes":
		payload = `{"findScenes":{"scenes":[{"id":"7","title":"Seven","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"seven.mp4","duration":1000,"path":"/seven.mp4","height":1080,"video_codec":"h264"}],"paths":{"stream":"http://stash/scene/7/stream"},"tags":[]}]}}`
	case "SceneSaveActivity":
		// req.Variables is *gql.__SceneSaveActivityInput, which is unexported
		// and unreachable from this package, so decode it structurally instead.
		var in struct {
			Seconds *float64 `json:"seconds"`
			Resume  *float64 `json:"resume"`
		}
		b, err := json.Marshal(req.Variables)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &in); err != nil {
			return err
		}
		f.mu.Lock()
		if in.Resume != nil {
			f.resumes = append(f.resumes, *in.Resume)
		}
		if in.Seconds != nil {
			f.withSeconds++
		}
		f.mu.Unlock()
		payload = `{"sceneSaveActivity":true}`
	default:
		payload = `{"sceneSaveActivity":true}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func TestResumePosition(t *testing.T) {
	cases := []struct{ duration, position, want float64 }{
		{1000, 600, 600},
		{1000, 990, 0}, // inside the last 3 percent
		{1000, 970, 0}, // exactly at 97 percent
		{1000, 3, 0},   // inside the first 5 seconds
		{0, 100, 0},    // unknown duration
	}
	for _, c := range cases {
		if got := resumePosition(c.duration, c.position); got != c.want {
			t.Errorf("resumePosition(%v, %v) = %v, want %v", c.duration, c.position, got, c.want)
		}
	}
}

func postEvent(t *testing.T, h *httpHandler, ev event, at float64) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": "http://x/heresphere/7", "event": int(ev), "time": at})
	req := httptest.NewRequest(http.MethodPost, "/events/7", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.eventsHandler(rec, req)
	return rec
}

func TestEvents_PauseAndCloseSaveResumePosition(t *testing.T) {
	stash := &fakeStash{}
	h := &httpHandler{libraryService: library.NewService(stash)}

	postEvent(t, h, evPause, 600)
	postEvent(t, h, evClose, 990)
	postEvent(t, h, evPause, 3)

	want := []float64{600, 0, 0}
	if len(stash.resumes) != len(want) {
		t.Fatalf("expected %d resume saves, got %v", len(want), stash.resumes)
	}
	for i := range want {
		if stash.resumes[i] != want[i] {
			t.Fatalf("save %d: got %v, want %v", i, stash.resumes[i], want[i])
		}
	}
}

func TestEvents_PlayDoesNotSaveResumePosition(t *testing.T) {
	stash := &fakeStash{}
	h := &httpHandler{libraryService: library.NewService(stash)}

	postEvent(t, h, evPlay, 120)

	if len(stash.resumes) != 0 {
		t.Fatalf("play must not save a resume position, got %v", stash.resumes)
	}
}

// Because saveActivity runs synchronously in the handler (detached context,
// not a goroutine), the fake sees the call before postEvent returns.
func TestEvents_PlayThenPauseReportsDurationAndResumeTogether(t *testing.T) {
	stash := &fakeStash{}
	h := &httpHandler{libraryService: library.NewService(stash)}

	postEvent(t, h, evPlay, 100)
	postEvent(t, h, evPause, 600)

	if len(stash.resumes) != 1 || stash.resumes[0] != 600 {
		t.Fatalf("expected one resume save of 600, got %v", stash.resumes)
	}
	if stash.withSeconds != 1 {
		t.Fatalf("expected the same call to carry the played seconds, got %d", stash.withSeconds)
	}
}

func TestVideoData_StoresProfileFromRequest(t *testing.T) {
	loadDefaultRules(t)
	h := &httpHandler{libraryService: library.NewService(&fakeStash{})}
	body, _ := json.Marshal(map[string]any{"hsp": base64.StdEncoding.EncodeToString([]byte("profile-bytes"))})
	req := httptest.NewRequest(http.MethodPost, "/7", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{URLParams: chi.RouteParams{Keys: []string{"videoId"}, Values: []string{"7"}}}))
	rec := httptest.NewRecorder()

	h.videoDataHandler(rec, req)
	waitFor(t, func() bool { return h.libraryService.HasProfile("7") })

	data, _ := os.ReadFile(h.libraryService.ProfilePath("7"))
	if rec.Code != 200 || string(data) != "profile-bytes" {
		t.Fatalf("code %d, stored %q", rec.Code, data)
	}

	bad, _ := json.Marshal(map[string]any{"hsp": "%%%not-base64"})
	req = httptest.NewRequest(http.MethodPost, "/7", bytes.NewReader(bad))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{URLParams: chi.RouteParams{Keys: []string{"videoId"}, Values: []string{"7"}}}))
	h.videoDataHandler(httptest.NewRecorder(), req)
	time.Sleep(50 * time.Millisecond)
	data, _ = os.ReadFile(h.libraryService.ProfilePath("7"))
	if string(data) != "profile-bytes" {
		t.Fatalf("malformed hsp must not overwrite the profile, got %q", data)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
