package heresphere

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"
	"stash-vr/internal/hsp"
	"stash-vr/internal/library"
)

// fakeStash answers the scene lookup for any id (every scene lasts 1000 s)
// and records every activity save: each resume position saved (skipping
// saves that carried none), how many saves also carried played seconds,
// the seconds played per scene and the play count increments per scene.
type fakeStash struct {
	mu          sync.Mutex
	resumes     []float64
	withSeconds int
	seconds     map[string]float64
	playCounts  map[string]int
	// minPlayPercent is Stash's minimumPlayPercent UI setting (nil for
	// unset), uiErr fails the configuration query and uiQueries counts it.
	minPlayPercent any
	uiErr          error
	uiQueries      int
}

func (f *fakeStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	// req.Variables is genqlient's unexported input struct, unreachable
	// from this package, so decode it structurally instead.
	vars := func(into any) error {
		b, err := json.Marshal(req.Variables)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, into)
	}
	var payload string
	switch req.OpName {
	case "FindScenes":
		var in struct {
			Ids []int `json:"scene_ids"`
		}
		if err := vars(&in); err != nil {
			return err
		}
		scenes := make([]string, 0, len(in.Ids))
		for _, id := range in.Ids {
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"Scene %d","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s%d.mp4","duration":1000,"path":"/s%d.mp4","height":1080,"video_codec":"h264"}],"paths":{"stream":"http://stash/scene/%d/stream"},"tags":[]}`, id, id, id, id, id))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	case "SceneSaveActivity":
		var in struct {
			Id      string   `json:"id"`
			Seconds *float64 `json:"seconds"`
			Resume  *float64 `json:"resume"`
		}
		if err := vars(&in); err != nil {
			return err
		}
		f.mu.Lock()
		if in.Resume != nil {
			f.resumes = append(f.resumes, *in.Resume)
		}
		if in.Seconds != nil {
			f.withSeconds++
			if f.seconds == nil {
				f.seconds = map[string]float64{}
			}
			f.seconds[in.Id] += *in.Seconds
		}
		f.mu.Unlock()
		payload = `{"sceneSaveActivity":true}`
	case "SceneIncrementPlayCount":
		var in struct {
			Id string `json:"id"`
		}
		if err := vars(&in); err != nil {
			return err
		}
		f.mu.Lock()
		if f.playCounts == nil {
			f.playCounts = map[string]int{}
		}
		f.playCounts[in.Id]++
		f.mu.Unlock()
		payload = `{"sceneAddPlay":null}`
	case "UIConfiguration":
		f.mu.Lock()
		f.uiQueries++
		err, pct := f.uiErr, f.minPlayPercent
		f.mu.Unlock()
		if err != nil {
			return err
		}
		ui, _ := json.Marshal(map[string]any{"minimumPlayPercent": pct})
		payload = `{"configuration":{"ui":` + string(ui) + `}}`
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindAllSceneIds", "FindSceneIdsByFilter":
		payload = `{"findScenes":{"scenes":[{"id":"7"}]}}`
	default:
		payload = `{"sceneSaveActivity":true}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

// playedSeconds is the seconds reported for scene id so far.
func (f *fakeStash) playedSeconds(id string) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seconds[id]
}

// configQueries is how often the UI configuration was asked for.
func (f *fakeStash) configQueries() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uiQueries
}

// setMinPlayPercent changes the setting and whether the query fails.
func (f *fakeStash) setMinPlayPercent(pct any, err error) {
	f.mu.Lock()
	f.minPlayPercent, f.uiErr = pct, err
	f.mu.Unlock()
}

// playCount is how often the play count of scene id was incremented.
func (f *fakeStash) playCount(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.playCounts[id]
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
	h := newHttpHandler(library.NewService(stash))

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
	h := newHttpHandler(library.NewService(stash))

	postEvent(t, h, evPlay, 120)

	if len(stash.resumes) != 0 {
		t.Fatalf("play must not save a resume position, got %v", stash.resumes)
	}
}

// Because saveActivity runs synchronously in the handler (detached context,
// not a goroutine), the fake sees the call before postEvent returns.
func TestEvents_PlayThenPauseReportsDurationAndResumeTogether(t *testing.T) {
	stash := &fakeStash{}
	h := newHttpHandler(library.NewService(stash))

	postEvent(t, h, evPlay, 100)
	postEvent(t, h, evPause, 600)

	if len(stash.resumes) != 1 || stash.resumes[0] != 600 {
		t.Fatalf("expected one resume save of 600, got %v", stash.resumes)
	}
	if stash.withSeconds != 1 {
		t.Fatalf("expected the same call to carry the played seconds, got %d", stash.withSeconds)
	}
}

// encodedProfile is a valid profile file titled title.
func encodedProfile(t *testing.T, title string) []byte {
	t.Helper()
	p := hsp.Default()
	p.Title = title
	data, err := hsp.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// postProfile sends a scene request for scene 7 whose hsp field is value.
func postProfile(t *testing.T, h *httpHandler, value string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hsp": value})
	req := httptest.NewRequest(http.MethodPost, "/7", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{URLParams: chi.RouteParams{Keys: []string{"videoId"}, Values: []string{"7"}}}))
	rec := httptest.NewRecorder()
	h.videoDataHandler(rec, req)
	return rec
}

func TestVideoData_StoresProfileFromRequest(t *testing.T) {
	loadDefaultRules(t)
	h := newHttpHandler(library.NewService(&fakeStash{}))
	good := encodedProfile(t, "Tuned")

	rec := postProfile(t, h, base64.StdEncoding.EncodeToString(good))
	waitFor(t, func() bool { return h.libraryService.HasProfile("7") })

	data, _ := os.ReadFile(h.libraryService.ProfilePath("7"))
	if rec.Code != 200 || !bytes.Equal(data, good) {
		t.Fatalf("code %d, stored %d bytes", rec.Code, len(data))
	}

	// Neither bad base64 nor a payload that is not a profile may replace
	// the stored one.
	for name, value := range map[string]string{
		"not base64":    "%%%not-base64",
		"not a profile": base64.StdEncoding.EncodeToString([]byte("profile-bytes")),
		"truncated":     base64.StdEncoding.EncodeToString(good[:len(good)-5]),
	} {
		if rec := postProfile(t, h, value); rec.Code != 200 {
			t.Fatalf("%s: the scene document must still be served, got %d", name, rec.Code)
		}
		time.Sleep(50 * time.Millisecond)
		data, _ = os.ReadFile(h.libraryService.ProfilePath("7"))
		if !bytes.Equal(data, good) {
			t.Fatalf("%s: must not overwrite the profile, stored %d bytes", name, len(data))
		}
	}

	// A valid replacement still lands.
	next := encodedProfile(t, "Retuned")
	postProfile(t, h, base64.StdEncoding.EncodeToString(next))
	waitFor(t, func() bool {
		data, _ := os.ReadFile(h.libraryService.ProfilePath("7"))
		return bytes.Equal(data, next)
	})
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

func TestVideoData_OversizedBodyIsIgnored(t *testing.T) {
	loadDefaultRules(t)
	h := newHttpHandler(library.NewService(&fakeStash{}))
	huge := strings.Repeat("A", maxVideoDataBody+1024)
	body, _ := json.Marshal(map[string]any{"hsp": huge})
	req := httptest.NewRequest(http.MethodPost, "/7", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{URLParams: chi.RouteParams{Keys: []string{"videoId"}, Values: []string{"7"}}}))
	rec := httptest.NewRecorder()

	h.videoDataHandler(rec, req)
	time.Sleep(50 * time.Millisecond)

	if rec.Code != 200 {
		t.Fatalf("the scene document must still be served, got %d", rec.Code)
	}
	if h.libraryService.HasProfile("7") {
		t.Fatal("an oversized body must not store a profile")
	}
}
