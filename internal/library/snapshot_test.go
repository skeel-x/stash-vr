package library

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

// sparseStash indexes scenes 1 and 2 but only ever returns scene 1 from
// FindScenes, as Stash does for a scene deleted since the index was built.
type sparseStash struct{}

func (s *sparseStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindAllSceneIds", "FindSceneIdsByFilter":
		payload = `{"findScenes":{"scenes":[{"id":"1"},{"id":"2"}]}}`
	case "FindScenes":
		payload = `{"findScenes":{"scenes":[{"id":"1","title":"One","created_at":"2024-01-01T00:00:00Z","files":[],"tags":[]}]}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func TestGetScenes_DropsScenesStashDidNotReturn(t *testing.T) {
	loadConfig(t, nil)
	svc := NewService(&sparseStash{})

	vds, err := svc.GetScenes(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := vds["2"]; ok {
		t.Fatalf("scene 2 was never fetched and must not appear as nil, got %v", vds)
	}
	if vd := vds["1"]; vd == nil || vd.Title() != "One" {
		t.Fatalf("expected scene 1, got %v", vds)
	}
	for id, vd := range vds {
		if vd == nil {
			t.Fatalf("snapshot holds a nil scene under %q", id)
		}
	}
}
