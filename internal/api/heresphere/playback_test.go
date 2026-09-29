package heresphere

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
)

// playbackClock is a settable clock for the tracker.
type playbackClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *playbackClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *playbackClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// clockedHandler is a handler whose tracker reads the returned clock.
func clockedHandler(stash *fakeStash) (*httpHandler, *playbackClock) {
	h := newHttpHandler(library.NewService(stash))
	clock := &playbackClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	h.playback.now = clock.now
	return h, clock
}

// postClientEvent posts ev for scene id from the client identified by
// connectionKey (empty for none) at position at.
func postClientEvent(t *testing.T, h *httpHandler, connectionKey, id string, ev event, at float64) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": "http://x/heresphere/" + id, "event": int(ev), "time": at, "connectionKey": connectionKey})
	req := httptest.NewRequest(http.MethodPost, "/events/"+id, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.eventsHandler(rec, req)
	return rec
}

func TestEvents_PlayTimeIsAttributedPerClient(t *testing.T) {
	stash := &fakeStash{}
	h, clock := clockedHandler(stash)

	// Headset a plays 7; ten seconds later headset b plays 8.
	postClientEvent(t, h, "a", "7", evPlay, 0)
	clock.advance(10 * time.Second)
	postClientEvent(t, h, "b", "8", evPlay, 0)
	// a pauses after 100 s of play, b after 120 s.
	clock.advance(90 * time.Second)
	postClientEvent(t, h, "a", "7", evPause, 100)
	clock.advance(30 * time.Second)
	postClientEvent(t, h, "b", "8", evPause, 120)

	if got := stash.playedSeconds("7"); got != 100 {
		t.Fatalf("scene 7: expected 100 s played by client a, got %v", got)
	}
	if got := stash.playedSeconds("8"); got != 120 {
		t.Fatalf("scene 8: expected 120 s played by client b, got %v", got)
	}
	if h.playback.size() != 2 {
		t.Fatalf("expected two tracked clients, got %d", h.playback.size())
	}
}

func TestEvents_SwitchingScenesReportsThePreviousOne(t *testing.T) {
	stash := &fakeStash{}
	h, clock := clockedHandler(stash)

	postClientEvent(t, h, "a", "7", evPlay, 0)
	clock.advance(40 * time.Second)
	postClientEvent(t, h, "a", "8", evPlay, 0)
	clock.advance(25 * time.Second)
	postClientEvent(t, h, "a", "8", evClose, 25)

	if got := stash.playedSeconds("7"); got != 40 {
		t.Fatalf("scene 7: expected the 40 s before the switch, got %v", got)
	}
	if got := stash.playedSeconds("8"); got != 25 {
		t.Fatalf("scene 8: expected 25 s, got %v", got)
	}
	if len(stash.resumes) != 1 || stash.resumes[0] != 25 {
		t.Fatalf("only the close carries a resume position, got %v", stash.resumes)
	}
}

func TestEvents_StopForAnotherSceneDoesNotStealPlayTime(t *testing.T) {
	stash := &fakeStash{}
	h, clock := clockedHandler(stash)

	postClientEvent(t, h, "a", "7", evPlay, 0)
	clock.advance(30 * time.Second)
	// A pause for a scene this client is not playing here: its position
	// is stored, but no play time is charged to it and 7 keeps playing.
	postClientEvent(t, h, "a", "8", evPause, 300)
	clock.advance(30 * time.Second)
	postClientEvent(t, h, "a", "7", evPause, 60)

	if got := stash.playedSeconds("8"); got != 0 {
		t.Fatalf("scene 8 was never played, got %v s", got)
	}
	if got := stash.playedSeconds("7"); got != 60 {
		t.Fatalf("scene 7: expected 60 s of uninterrupted play, got %v", got)
	}
	if len(stash.resumes) != 2 || stash.resumes[0] != 300 || stash.resumes[1] != 60 {
		t.Fatalf("expected both positions stored, got %v", stash.resumes)
	}
}

func TestEvents_PauseAndResumeAccumulate(t *testing.T) {
	stash := &fakeStash{}
	h, clock := clockedHandler(stash)

	postClientEvent(t, h, "a", "7", evPlay, 0)
	clock.advance(20 * time.Second)
	postClientEvent(t, h, "a", "7", evPause, 20)
	clock.advance(time.Hour) // paused: does not count
	postClientEvent(t, h, "a", "7", evPlay, 20)
	clock.advance(15 * time.Second)
	postClientEvent(t, h, "a", "7", evClose, 35)

	if got := stash.playedSeconds("7"); got != 35 {
		t.Fatalf("expected 20 + 15 s, got %v", got)
	}
}

func TestEvents_IdleClientsAreSwept(t *testing.T) {
	stash := &fakeStash{}
	h, clock := clockedHandler(stash)

	postClientEvent(t, h, "gone", "7", evPlay, 0)
	postClientEvent(t, h, "here", "8", evPlay, 0)
	clock.advance(playbackIdleTTL - time.Minute)
	postClientEvent(t, h, "here", "8", evPause, 10)
	if h.playback.size() != 2 {
		t.Fatalf("nothing is idle yet, got %d clients", h.playback.size())
	}

	// "gone" has now been silent for the TTL; the next event after the
	// sweep interval sweeps it.
	clock.advance(playbackSweepEvery + time.Minute)
	postClientEvent(t, h, "here", "8", evPlay, 10)
	if h.playback.size() != 1 {
		t.Fatalf("expected the idle client dropped, got %d clients", h.playback.size())
	}
	// A stop from the swept client is not charged: its time is unknown.
	postClientEvent(t, h, "gone", "7", evPause, 10)
	if got := stash.playedSeconds("7"); got != 0 {
		t.Fatalf("a swept client must not report play time, got %v", got)
	}
}

func TestPlaybackTracker_SweepIsRateLimited(t *testing.T) {
	tr := newPlaybackTracker()
	clock := &playbackClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	tr.now = clock.now
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "7", Files: []*gql.ScenePartsFilesVideoFile{{Duration: 1000}}}}

	tr.play("a", vd, nil)
	clock.advance(playbackIdleTTL - 5*time.Minute)
	// This event sweeps (the last sweep is long past) but a is not idle yet.
	tr.play("b", vd, nil)
	if tr.size() != 2 {
		t.Fatalf("a is not idle yet, got %d", tr.size())
	}
	// a is idle now, but within playbackSweepEvery no sweep runs.
	clock.advance(5 * time.Minute)
	tr.play("c", vd, nil)
	if tr.size() != 3 {
		t.Fatalf("expected no sweep inside the sweep interval, got %d", tr.size())
	}
	clock.advance(5 * time.Minute)
	tr.play("c", vd, nil)
	if tr.size() != 2 {
		t.Fatalf("expected a swept once the interval passed, got %d", tr.size())
	}
}

func TestPlaybackState_HandleStopCountsPlayOnce(t *testing.T) {
	half := 0.5
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "7", Files: []*gql.ScenePartsFilesVideoFile{{Duration: 100}}}}
	ps := newPlayback(vd, start)

	stop := ps.handleStop(start.Add(30*time.Second), &half)
	if stop.played != 30 || stop.countPlay {
		t.Fatalf("30 of 100 s must not count as played: %+v", stop)
	}
	if stop := ps.handleStop(start.Add(31*time.Second), &half); stop.played != 0 || stop.countPlay {
		t.Fatalf("a stop while stopped reports nothing: %+v", stop)
	}
	ps.handleResume(start.Add(time.Minute))
	stop = ps.handleStop(start.Add(90*time.Second), &half)
	if stop.played != 30 || !stop.countPlay {
		t.Fatalf("60 of 100 s crosses the threshold: %+v", stop)
	}
	ps.handleResume(start.Add(100 * time.Second))
	if stop := ps.handleStop(start.Add(200*time.Second), &half); stop.played != 100 || stop.countPlay {
		t.Fatalf("the play count is bumped once per open: %+v", stop)
	}
	ps.handleResume(start.Add(300 * time.Second))
	if stop := ps.handleStop(start.Add(400*time.Second), nil); stop.countPlay {
		t.Fatalf("no threshold, no count: %+v", stop)
	}
	if stop.videoId != "7" {
		t.Fatalf("stop names the scene, got %q", stop.videoId)
	}
}

func TestEvents_ThresholdIncrementsPlayCountOnce(t *testing.T) {
	stash := &fakeStash{}
	h, clock := clockedHandler(stash)
	half := 0.5
	minPlayFraction = &half
	t.Cleanup(func() { minPlayFraction = nil })

	postClientEvent(t, h, "a", "7", evPlay, 0)
	clock.advance(400 * time.Second)
	postClientEvent(t, h, "a", "7", evPause, 400)
	if got := stash.playCount("7"); got != 0 {
		t.Fatalf("400 of 1000 s must not count, got %d", got)
	}
	postClientEvent(t, h, "a", "7", evPlay, 400)
	clock.advance(200 * time.Second)
	postClientEvent(t, h, "a", "7", evPause, 600)
	postClientEvent(t, h, "a", "7", evPlay, 600)
	clock.advance(300 * time.Second)
	postClientEvent(t, h, "a", "7", evClose, 900)
	if got := stash.playCount("7"); got != 1 {
		t.Fatalf("expected one play counted, got %d", got)
	}
	if got := stash.playedSeconds("7"); got != 900 {
		t.Fatalf("expected 900 s played, got %v", got)
	}
}

func TestPlaybackState_NoFilesMeansNoDuration(t *testing.T) {
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "7"}}
	ps := newPlayback(vd, time.Now())
	if ps.videoDuration != 0 || ps.videoId != "7" {
		t.Fatalf("unexpected state %+v", ps)
	}
}

func TestClientKey(t *testing.T) {
	cases := []struct {
		name   string
		ev     playbackEvent
		remote string
		want   string
	}{
		{"connection key first", playbackEvent{ConnectionKey: "k1", Username: "erik"}, "10.0.0.1:1234", "connection:k1"},
		{"then user name", playbackEvent{Username: "erik"}, "10.0.0.1:1234", "user:erik"},
		{"then the peer address without port", playbackEvent{}, "10.0.0.1:1234", "ip:10.0.0.1"},
		{"ipv6 peer", playbackEvent{}, "[::1]:1234", "ip:::1"},
		{"bare address", playbackEvent{}, "10.0.0.1", "ip:10.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/events/7", nil)
			req.RemoteAddr = c.remote
			if got := clientKey(req, &c.ev); got != c.want {
				t.Fatalf("clientKey = %q, want %q", got, c.want)
			}
		})
	}
}

// Run under -race: many headsets hammering the event server at once.
func TestEvents_ConcurrentClients(t *testing.T) {
	stash := &fakeStash{}
	h := newHttpHandler(library.NewService(stash))
	const clients, rounds = 8, 20

	var wg sync.WaitGroup
	for c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("c%d", c)
			for r := range rounds {
				id := fmt.Sprint(7 + r%3)
				if rec := postClientEvent(t, h, key, id, evPlay, 0); rec.Code != http.StatusOK {
					t.Errorf("play: %d", rec.Code)
				}
				if rec := postClientEvent(t, h, key, id, evPause, 100); rec.Code != http.StatusOK {
					t.Errorf("pause: %d", rec.Code)
				}
			}
		}()
	}
	wg.Wait()

	if h.playback.size() != clients {
		t.Fatalf("expected %d tracked clients, got %d", clients, h.playback.size())
	}
	if len(stash.resumes) != clients*rounds {
		t.Fatalf("expected %d resume saves, got %d", clients*rounds, len(stash.resumes))
	}
}
