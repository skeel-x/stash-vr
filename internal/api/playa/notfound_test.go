package playa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/go-chi/chi/v5"

	"stash-vr/internal/library"
)

// fakeNotFoundGraphQL answers FindScenes with no scenes and FindSavedSceneFilters
// with no saved filters, regardless of call order, so it is safe for the
// concurrent GetScene/GetSavedFilterSceneSets calls videoHandler makes.
type fakeNotFoundGraphQL struct{}

func (f *fakeNotFoundGraphQL) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	payload := `{"findScenes":{"scenes":[]}}`
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func newPlayaRequestWithVideoId(videoId string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("videoId", videoId)
	req := httptest.NewRequest(http.MethodGet, "/api/playa/v2/video/"+videoId, nil)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestPosterHandler_UnknownIdReturns404(t *testing.T) {
	svc := library.NewService(&fakeNotFoundGraphQL{})
	h := httpHandler{libraryService: svc}

	req := newPlayaRequestWithVideoId("999999")
	w := httptest.NewRecorder()

	h.posterHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body=%q", w.Code, w.Body.String())
	}
}

func TestVideoHandler_UnknownIdReturnsNotFoundEnvelope(t *testing.T) {
	svc := library.NewService(&fakeNotFoundGraphQL{})
	h := httpHandler{libraryService: svc}

	req := newPlayaRequestWithVideoId("999999")
	w := httptest.NewRecorder()

	h.videoHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 envelope, got %d, body=%q", w.Code, w.Body.String())
	}

	var body struct {
		Status struct {
			Code int `json:"code"`
		} `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response body %q: %v", w.Body.String(), err)
	}
	if body.Status.Code != statusNotFound {
		t.Fatalf("expected status.code %d, got %d, body=%q", statusNotFound, body.Status.Code, w.Body.String())
	}
}
