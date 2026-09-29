package internal

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// hungStash accepts every request and never answers, standing in for a
// Stash that is still indexing. It records the deadline of the first
// request it saw.
type hungStash struct {
	mu          sync.Mutex
	calls       int
	hasDeadline bool
}

func (h *hungStash) MakeRequest(ctx context.Context, _ *graphql.Request, _ *graphql.Response) error {
	_, hasDeadline := ctx.Deadline()
	h.mu.Lock()
	if h.calls == 0 {
		h.hasDeadline = hasDeadline
	}
	h.calls++
	h.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (h *hungStash) state() (int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls, h.hasDeadline
}

// freeAddress returns a loopback address nothing listens on right now.
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func loadTestConfig(t *testing.T, listenAddress string) {
	t.Helper()
	err := config.Load(config.ApplicationConfig{
		ListenAddress:    listenAddress,
		StashGraphQLUrl:  "http://stash:9999/graphql",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		ConfigPath:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestServe_SetupPageReachableWhileStashHangs(t *testing.T) {
	addr := freeAddress(t)
	loadTestConfig(t, addr)
	stash := &hungStash{}
	lib := library.NewService(stash)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, addr, stash, lib) }()

	client := &http.Client{Timeout: time.Second}
	waitFor(t, "the setup page", func() bool {
		resp, err := client.Get("http://" + addr + "/setup")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	// Stash is still hanging in the warmup, under a deadline.
	waitFor(t, "the warmup to probe stash", func() bool { calls, _ := stash.state(); return calls > 0 })
	if _, hasDeadline := stash.state(); !hasDeadline {
		t.Fatal("the warmup must probe Stash under a deadline")
	}
	select {
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	default:
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned an error on shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not return after the context ended")
	}
	// The sweeper was joined before serve returned, so its final flush is
	// already on disk by the time the process exits.
	if !lib.WaitDateSweeper(0) {
		t.Fatal("serve returned before the date sweeper stopped")
	}
}

func TestServe_ListenFailureIsReported(t *testing.T) {
	addr := freeAddress(t)
	loadTestConfig(t, addr)
	// Occupy the port so the server cannot bind it.
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stash := &hungStash{}
	lib := library.NewService(stash)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, addr, stash, lib) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a listen error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not return after the listen failure")
	}
	// The sweeper is stopped with the server, not left to the process exit.
	if !lib.WaitDateSweeper(0) {
		t.Fatal("serve returned before the date sweeper stopped")
	}
}
