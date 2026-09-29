package library

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog"
	"stash-vr/internal/config"
)

// failingStash wraps a fake Stash and fails the operations listed in fail,
// standing in for a Stash that is down or answers nothing.
type failingStash struct {
	inner graphql.Client
	mu    sync.Mutex
	fail  map[string]error
}

func (f *failingStash) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	f.mu.Lock()
	err := f.fail[req.OpName]
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.inner.MakeRequest(ctx, req, resp)
}

func (f *failingStash) set(fail map[string]error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
}

var errStashDown = errors.New("stash down")

// The default index has four smart sections: three query scene ids by
// filter, one (Recommended) asks for recommendation scenes.
var everySectionQuery = map[string]error{"FindSceneIdsByFilter": errStashDown, "FindRecommendationScenes": errStashDown}

func TestGetSections_FailsWhenEverySectionQueryFails(t *testing.T) {
	cases := []struct {
		name         string
		configure    func(t *testing.T)
		groupings    string
		fail         map[string]error
		wantErr      string
		wantSections int
	}{
		{
			name:      "every query fails",
			configure: func(t *testing.T) { loadConfig(t, nil) },
			fail:      everySectionQuery,
			wantErr:   "every section query failed (4 of 4)",
		},
		{
			name:         "one query fails",
			configure:    func(t *testing.T) { loadConfig(t, nil) },
			fail:         map[string]error{"FindRecommendationScenes": errStashDown},
			wantSections: 3,
		},
		{
			name:      "every query fails with auto sections present",
			configure: func(t *testing.T) { loadAutoConfig(t, 2, 0, nil) },
			groupings: twoStudioGroupings,
			fail:      everySectionQuery,
			wantErr:   "every section query failed (4 of 4)",
		},
		{
			name: "only auto sections need no query",
			configure: func(t *testing.T) {
				loadAutoConfig(t, 2, 0, []config.Filter{
					{ID: "smart:continue", Disabled: true}, {ID: "smart:recommended", Disabled: true},
					{ID: "smart:recent", Disabled: true}, {ID: "smart:random", Disabled: true},
				})
			},
			groupings:    twoStudioGroupings,
			fail:         everySectionQuery,
			wantSections: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.configure(t)
			stash := &failingStash{inner: &routingStash{groupings: c.groupings}, fail: c.fail}
			svc := NewService(stash)

			sections, err := svc.GetSections(context.Background())
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got %v (sections %v)", c.wantErr, err, names(sections))
				}
				if _, cached := svc.freshIndex(); cached {
					t.Fatal("a failed build must not be cached as an empty library")
				}
				// Stash is back: the next request rebuilds without waiting
				// out the TTL.
				stash.set(nil)
				sections, err = svc.GetSections(context.Background())
				if err != nil || len(sections) == 0 {
					t.Fatalf("expected a rebuild once Stash answers, got %v %v", names(sections), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(sections) != c.wantSections {
				t.Fatalf("expected %d sections, got %v", c.wantSections, names(sections))
			}
		})
	}
}

func TestGetSections_ServesPreviousIndexWhenRebuildFails(t *testing.T) {
	loadConfig(t, nil)
	stash := &failingStash{inner: &routingStash{}}
	svc := NewService(stash)

	before, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The index is past its TTL and Stash has stopped answering.
	svc.muSets.Lock()
	svc.setsAt = time.Now().Add(-2 * sectionCacheTTL)
	svc.muSets.Unlock()
	stash.set(map[string]error{"FindSavedSceneFilters": errStashDown})

	after, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatalf("a failed rebuild must serve the previous index, got %v", err)
	}
	if got, want := names(after), names(before); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected the previous index %v, got %v", want, got)
	}
	if _, cached := svc.freshIndex(); cached {
		t.Fatal("the previous index must not be marked fresh by a failed rebuild")
	}

	// A reset drops the previous index: nothing is left to fall back on.
	svc.ResetSections()
	if _, err := svc.GetSections(context.Background()); !errors.Is(err, errStashDown) {
		t.Fatalf("expected the rebuild error after a reset, got %v", err)
	}
}

func TestIndex_WarnsWhenTagRefreshFails(t *testing.T) {
	loadConfig(t, nil)
	stash := &failingStash{inner: &routingStash{}, fail: map[string]error{"FindAllTags": errStashDown}}
	svc := NewService(stash)

	// The section resolvers log from their own goroutines.
	var buf lockedBuffer
	ctx := zerolog.New(&buf).WithContext(context.Background())

	sections, err := svc.GetSections(ctx)
	if err != nil {
		t.Fatalf("a tag refresh failure must not fail the index build, got %v", err)
	}
	if len(sections) == 0 {
		t.Fatal("expected the sections to be built without the tags")
	}
	out := buf.String()
	if !strings.Contains(out, `"level":"warn"`) || !strings.Contains(out, errStashDown.Error()) {
		t.Fatalf("expected a warning carrying the tag error, got log %q", out)
	}
}

// lockedBuffer is a bytes.Buffer safe to log to from several goroutines.
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
