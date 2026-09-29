package heresphere

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/library"
	"stash-vr/internal/util"
)

// gatedStash answers scene lookups and holds every rating update until
// release is closed, counting how many are held at once and recording the
// order of every operation.
type gatedStash struct {
	mu       sync.Mutex
	release  chan struct{}
	inflight int
	maxIn    int
	ops      []string
}

func (g *gatedStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	g.mu.Lock()
	g.ops = append(g.ops, req.OpName)
	g.mu.Unlock()
	payload := `{}`
	switch req.OpName {
	case "FindScenes":
		var in struct {
			Ids []int `json:"scene_ids"`
		}
		b, _ := json.Marshal(req.Variables)
		_ = json.Unmarshal(b, &in)
		scenes := make([]string, 0, len(in.Ids))
		for _, id := range in.Ids {
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"Scene %d","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s.mp4","duration":1000,"path":"/s.mp4","height":1080}],"tags":[]}`, id, id))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	case "SceneUpdateRating100":
		g.mu.Lock()
		g.inflight++
		g.maxIn = max(g.maxIn, g.inflight)
		g.mu.Unlock()
		<-g.release
		g.mu.Lock()
		g.inflight--
		g.mu.Unlock()
		payload = `{"sceneUpdate":{"id":"7"}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func (g *gatedStash) held() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight
}

func (g *gatedStash) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.ops...)
}

func TestProcessUpdates_SameSceneRunsOneAtATime(t *testing.T) {
	loadDefaultRules(t)
	stash := &gatedStash{release: make(chan struct{})}
	h := newHttpHandler(library.NewService(stash))
	req := videoDataRequestDto{Rating: util.Ptr(float32(4))}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.processUpdates(context.Background(), "7", req)
		}()
	}
	waitFor(t, func() bool { return stash.held() == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := stash.held(); got != 1 {
		t.Fatalf("the second update of the same scene must wait, %d in flight", got)
	}
	close(stash.release)
	wg.Wait()

	// Each update finishes, refetch included, before the next one starts.
	want := "SceneUpdateRating100 FindScenes SceneUpdateRating100 FindScenes"
	if got := strings.Join(stash.seen(), " "); got != want {
		t.Fatalf("ops = %q, want %q", got, want)
	}
	// The lock is free again once both are done.
	free := make(chan struct{})
	go func() {
		h.sceneLocks.Lock("7")()
		close(free)
	}()
	select {
	case <-free:
	case <-time.After(2 * time.Second):
		t.Fatal("the scene lock is still held")
	}
}

func TestProcessUpdates_DifferentScenesRunTogether(t *testing.T) {
	loadDefaultRules(t)
	stash := &gatedStash{release: make(chan struct{})}
	h := newHttpHandler(library.NewService(stash))
	req := videoDataRequestDto{Rating: util.Ptr(float32(4))}

	var wg sync.WaitGroup
	for _, id := range []string{"7", "8"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.processUpdates(context.Background(), id, req)
		}()
	}
	waitFor(t, func() bool { return stash.held() == 2 })
	close(stash.release)
	wg.Wait()
	if stash.maxIn != 2 {
		t.Fatalf("expected both scenes updated at once, max in flight %d", stash.maxIn)
	}
}
