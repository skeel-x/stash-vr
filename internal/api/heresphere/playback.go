package heresphere

import (
	"net"
	"net/http"
	"stash-vr/internal/library"
	"sync"
	"time"
)

const (
	// playbackIdleTTL is how long a client's playback state is kept after
	// its last event. A headset that vanished mid-scene never sends a stop,
	// so its state is dropped instead of living for the life of the process.
	playbackIdleTTL = 6 * time.Hour
	// playbackSweepEvery bounds how often idle states are looked for.
	playbackSweepEvery = 10 * time.Minute
)

// playbackTracker holds the playback state of every client that sends
// events, keyed by clientKey. Events from several headsets arrive
// concurrently, so every access goes through mu.
type playbackTracker struct {
	mu        sync.Mutex
	clients   map[string]*playbackState
	lastSweep time.Time
	// now is the clock; tests replace it.
	now func() time.Time
}

func newPlaybackTracker() *playbackTracker {
	return &playbackTracker{clients: map[string]*playbackState{}, now: time.Now}
}

// clientKey identifies the client an event came from: HereSphere's
// connection key, else its user name, else the peer address. The prefixes
// keep the namespaces apart.
func clientKey(req *http.Request, ev *playbackEvent) string {
	switch {
	case ev.ConnectionKey != "":
		return "connection:" + ev.ConnectionKey
	case ev.Username != "":
		return "user:" + ev.Username
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	return "ip:" + host
}

// playbackStop is what a client stopped playing: the scene, the seconds
// played since the last report and whether this stop crossed the play
// count threshold. The caller reports it to Stash outside the tracker's
// lock.
type playbackStop struct {
	videoId   string
	played    float64
	countPlay bool
}

// play records that client key started or resumed vd. When the client was
// playing another scene, that scene's stop is returned for reporting.
func (t *playbackTracker) play(key string, vd *library.VideoData, minPlayFraction *float64) *playbackStop {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.sweepLocked(now)
	ps := t.clients[key]
	if ps != nil && ps.videoId == vd.Id() {
		ps.handleResume(now)
		return nil
	}
	var prev *playbackStop
	if ps != nil {
		// Another scene started: report what was played of the previous
		// one; its resume position is unknown here, so leave it as is.
		prev = ps.handleStop(now, minPlayFraction)
	}
	t.clients[key] = newPlayback(vd, now)
	return prev
}

// stop records that client key paused or closed videoId and returns the
// stop to report, nil when the client was not playing that scene: a stop
// for one scene must not be charged to another one the client has open.
func (t *playbackTracker) stop(key, videoId string, minPlayFraction *float64) *playbackStop {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.sweepLocked(now)
	ps := t.clients[key]
	if ps == nil || ps.videoId != videoId {
		return nil
	}
	return ps.handleStop(now, minPlayFraction)
}

// sweepLocked drops the states of clients idle for playbackIdleTTL, at most
// once per playbackSweepEvery. t.mu is held.
func (t *playbackTracker) sweepLocked(now time.Time) {
	if now.Sub(t.lastSweep) < playbackSweepEvery {
		return
	}
	t.lastSweep = now
	for key, ps := range t.clients {
		if now.Sub(ps.lastEvent) >= playbackIdleTTL {
			delete(t.clients, key)
		}
	}
}

// size is how many clients are tracked, for tests.
func (t *playbackTracker) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.clients)
}

func newPlayback(vd *library.VideoData, now time.Time) *playbackState {
	ps := &playbackState{
		videoId:      vd.Id(),
		lastPlayTime: now,
		lastEvent:    now,
		isPlaying:    true,
	}
	if len(vd.SceneParts.Files) > 0 && vd.SceneParts.Files[0] != nil {
		ps.videoDuration = vd.SceneParts.Files[0].Duration
	}
	return ps
}

// handleStop marks playback stopped at now and returns the stop to report:
// the seconds played since the last report (0 when it was not playing) and
// whether the accumulated play time crossed minPlayFraction of the video's
// duration with this stop, which happens once per scene open.
func (ps *playbackState) handleStop(now time.Time, minPlayFraction *float64) *playbackStop {
	ps.lastEvent = now
	stop := &playbackStop{videoId: ps.videoId}
	if !ps.isPlaying {
		return stop
	}
	currentPlayDuration := now.Sub(ps.lastPlayTime)
	ps.accumulatedPlayTime += currentPlayDuration
	if !ps.thresholdReached && minPlayFraction != nil && ps.accumulatedPlayTime.Seconds() >= ps.videoDuration*(*minPlayFraction) {
		ps.thresholdReached = true
		stop.countPlay = true
	}
	ps.isPlaying = false
	stop.played = currentPlayDuration.Seconds()
	return stop
}

// resumePosition decides what to store as the scene's resume time after the
// player paused or closed at position seconds. Positions inside the first 5
// seconds or at or beyond 97 percent of the duration clear the stored
// position, so finished scenes leave "Continue watching".
func resumePosition(duration, position float64) float64 {
	if duration <= 0 || position < 5 || position >= duration*0.97 {
		return 0
	}
	return position
}

func (ps *playbackState) handleResume(now time.Time) {
	ps.lastEvent = now
	if !ps.isPlaying {
		ps.lastPlayTime = now
	}
	ps.isPlaying = true
}

type playbackState struct {
	videoId       string
	videoDuration float64

	accumulatedPlayTime time.Duration
	thresholdReached    bool
	lastPlayTime        time.Time
	// lastEvent is when the client last sent an event for this scene.
	lastEvent time.Time
	isPlaying bool
}
