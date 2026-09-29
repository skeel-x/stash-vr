package library

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
)

// panickingStash serves one saved filter and panics when asked for its
// scenes, standing in for any unexpected failure inside a section build.
type panickingStash struct{}

func (p *panickingStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[{"id":"1","name":"Boom","mode":"SCENES","find_filter":{"sort":"boom"},"object_filter":{}}]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindSceneIdsByFilter":
		raw, _ := json.Marshal(req.Variables)
		in := &sceneIdQuery{}
		_ = json.Unmarshal(raw, in)
		if in.FilterOpts != nil && in.FilterOpts.Sort != nil && *in.FilterOpts.Sort == "boom" {
			panic("section build exploded")
		}
		payload = `{"findScenes":{"scenes":[{"id":"1"},{"id":"2"}]}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func TestGetSections_PanickingSectionIsSkippedNotFatal(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: "1"}, {ID: "smart:recent"}})
	svc := NewService(&panickingStash{})

	sections, err := svc.GetSections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	got := names(sections)
	if slices.Contains(got, "Boom") {
		t.Fatalf("panicking section must be skipped, got %v", got)
	}
	if !slices.Contains(got, "Recently added") {
		t.Fatalf("other sections must still build, got %v", got)
	}
}
