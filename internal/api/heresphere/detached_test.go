package heresphere

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"stash-vr/internal/library"
)

// The background work a request starts must outlive the request (a headset
// dropping the connection must not abort it) but never run unbounded (a
// silent Stash must not pin it until a restart).
func TestBackgroundWork_DetachedFromRequestButBounded(t *testing.T) {
	rating := float32(4.5)
	cases := []struct {
		name string
		op   string // the Stash operation the background work makes
		run  func(h *httpHandler, reqCtx context.Context)
	}{
		{
			name: "index scene prefetch",
			op:   "FindScenes",
			run: func(h *httpHandler, reqCtx context.Context) {
				req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(reqCtx)
				rec := httptest.NewRecorder()
				h.indexHandler(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d", rec.Code)
				}
			},
		},
		{
			name: "scene updates",
			op:   "SceneUpdateRating100",
			run: func(h *httpHandler, reqCtx context.Context) {
				h.processUpdates(reqCtx, "7", videoDataRequestDto{Rating: &rating})
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loadDefaultRules(t)
			stash := &explodingStash{}
			h := &httpHandler{libraryService: library.NewService(stash)}

			// The request is already gone when the work runs.
			reqCtx, cancel := context.WithCancel(context.Background())
			cancel()
			c.run(h, reqCtx)

			waitFor(t, func() bool { return stash.sawOp(c.op) })
			state, _ := stash.ctxOf(c.op)
			if state.err != nil {
				t.Fatalf("%s must not inherit the request's cancellation, got %v", c.op, state.err)
			}
			if !state.hasDeadline {
				t.Fatalf("%s must run under a deadline", c.op)
			}
		})
	}
}
