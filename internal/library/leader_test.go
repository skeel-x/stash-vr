package library

import (
	"context"
	"sync"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

// ctxProbe wraps a fake Stash and behaves like a real client would: a
// request whose context is cancelled fails. It records the context state
// of every request so a test can tell what the singleflight leaders ran under.
type ctxProbe struct {
	inner graphql.Client
	mu    sync.Mutex
	seen  []probedRequest
}

type probedRequest struct {
	op          string
	err         error
	hasDeadline bool
}

func (p *ctxProbe) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	_, hasDeadline := ctx.Deadline()
	p.mu.Lock()
	p.seen = append(p.seen, probedRequest{op: req.OpName, err: ctx.Err(), hasDeadline: hasDeadline})
	p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.inner.MakeRequest(ctx, req, resp); err != nil {
		return err
	}
	return ctx.Err()
}

func (p *ctxProbe) requests(op string) []probedRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []probedRequest
	for _, r := range p.seen {
		if r.op == op {
			out = append(out, r)
		}
	}
	return out
}

// A singleflight leader works for every caller waiting on it, so it must
// not inherit the cancellation of the one request that happened to start
// it, and it must run under a deadline of its own.
func TestSingleflightLeaders_DetachedFromCallerButBounded(t *testing.T) {
	cases := []struct {
		name string
		op   string // a Stash operation only the leader makes
		call func(svc *Service, ctx context.Context) error
	}{
		{
			name: "index build",
			op:   "FindSceneIdsByFilter",
			call: func(svc *Service, ctx context.Context) error {
				sections, err := svc.GetSections(ctx)
				if err == nil && len(sections) != 3 {
					t.Fatalf("expected the full index, got %v", names(sections))
				}
				return err
			},
		},
		{
			name: "scene fetch",
			op:   "FindScenes",
			call: func(svc *Service, ctx context.Context) error {
				vds, err := svc.GetScenes(ctx)
				if err == nil && len(vds) != 3 {
					t.Fatalf("expected every scene fetched, got %d", len(vds))
				}
				return err
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loadConfig(t, nil)
			probe := &ctxProbe{inner: &routingStash{}}
			svc := NewService(probe)

			// The caller is gone before the leader even starts.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := c.call(svc, ctx); err != nil {
				t.Fatalf("the caller's cancellation must not fail the shared work: %v", err)
			}

			reqs := probe.requests(c.op)
			if len(reqs) == 0 {
				t.Fatalf("expected %s to be made", c.op)
			}
			for _, r := range reqs {
				if r.err != nil {
					t.Fatalf("%s inherited the caller's cancellation: %v", c.op, r.err)
				}
				if !r.hasDeadline {
					t.Fatalf("%s must run under a deadline", c.op)
				}
			}
		})
	}
}

// A caller that drops out mid-build must not truncate the index the other
// waiters receive.
func TestIndexBuild_SurvivesCallerCancelledMidBuild(t *testing.T) {
	loadConfig(t, nil)
	stash := &routingStash{gate: make(chan struct{}), started: make(chan struct{})}
	probe := &ctxProbe{inner: stash}
	svc := NewService(probe)

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		sections []Section
		err      error
	}
	done := make(chan result, 1)
	go func() {
		sections, err := svc.GetSections(ctx)
		done <- result{sections, err}
	}()

	<-stash.started
	cancel()
	close(stash.gate)

	got := <-done
	if got.err != nil {
		t.Fatalf("build failed after the caller left: %v", got.err)
	}
	if len(got.sections) != 3 {
		t.Fatalf("index truncated after the caller left: %v", names(got.sections))
	}
	if q := stash.sceneIdQueries(); q != 3 {
		t.Fatalf("expected every section query to complete, got %d", q)
	}
}
