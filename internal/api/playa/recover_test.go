package playa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Khan/genqlient/graphql"

	"stash-vr/internal/library"
)

// panickingGraphQL panics on FindScenes and answers everything else empty,
// standing in for a scene lookup that blows up inside videoHandler's
// detached goroutine.
type panickingGraphQL struct{}

func (p *panickingGraphQL) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	payload := `{}`
	switch req.OpName {
	case "FindScenes":
		panic("stash exploded")
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindAllSceneIds", "FindSceneIdsByFilter":
		payload = `{"findScenes":{"scenes":[]}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func TestVideoHandler_PanicInLookupAnswersNotFound(t *testing.T) {
	svc := library.NewService(&panickingGraphQL{})
	h := httpHandler{libraryService: svc}

	req := newPlayaRequestWithVideoId("7")
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
