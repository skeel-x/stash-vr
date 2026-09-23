package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
)

// indexStash answers every index query with the same id pool and FindScenes
// with a scene per requested id, so tests know exactly which scenes exist.
type indexStash struct{ ids []string }

func (f *indexStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindAllSceneIds", "FindSceneIdsByFilter":
		ids := make([]string, len(f.ids))
		for i, id := range f.ids {
			ids[i] = fmt.Sprintf(`{"id":"%s"}`, id)
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(ids, ",") + `]}}`
	case "FindScenes":
		raw, _ := json.Marshal(req.Variables)
		var in struct {
			SceneIDs []int `json:"scene_ids"`
		}
		_ = json.Unmarshal(raw, &in)
		scenes := make([]string, 0, len(in.SceneIDs))
		for _, id := range in.SceneIDs {
			scenes = append(scenes, fmt.Sprintf(`{"id":"%d","title":"Scene %d","created_at":"2024-01-01T00:00:00Z","files":[],"tags":[]}`, id, id))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(scenes, ",") + `]}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func TestGetScene_UnknownIdReturnsErrSceneNotFound(t *testing.T) {
	client := &fakeGraphQL{payloads: []string{
		`{"findScenes":{"scenes":[]}}`,
	}}
	svc := NewService(client)

	vd, err := svc.GetScene(context.Background(), "999999", false)

	if !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("expected ErrSceneNotFound, got vd=%v err=%v", vd, err)
	}
	if vd != nil {
		t.Fatalf("expected nil VideoData on not found, got %v", vd)
	}
}

func TestGetScene_NonNumericIdReturnsErrSceneNotFound(t *testing.T) {
	svc := NewService(&fakeGraphQL{})

	_, err := svc.GetScene(context.Background(), "not-a-number", false)

	if !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("expected ErrSceneNotFound for non-numeric id, got %v", err)
	}
}

func TestRandomScenes_DistinctFromIndex(t *testing.T) {
	// Default smart sections: continue, recent and random are enabled and
	// the fake answers each with ids 1..10.
	loadConfig(t, nil)
	s := NewService(&indexStash{ids: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}})

	got, err := s.RandomScenes(context.Background(), 6)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, vd := range got {
		if seen[vd.Id()] {
			t.Fatalf("duplicate id %s in %v", vd.Id(), got)
		}
		seen[vd.Id()] = true
		if vd.Title() != "Scene "+vd.Id() {
			t.Fatalf("scene %s carries title %q", vd.Id(), vd.Title())
		}
	}
	if len(got) != 6 {
		t.Fatalf("expected 6 scenes, got %d", len(got))
	}
	if few, _ := s.RandomScenes(context.Background(), 50); len(few) != 10 {
		t.Fatalf("expected all 10 when asking for more, got %d", len(few))
	}
}

func TestRandomScenes_FallsBackToEverySectionWithoutRandom(t *testing.T) {
	loadConfig(t, []config.Filter{{ID: "smart:random", Disabled: true}})
	s := NewService(&indexStash{ids: []string{"1", "2", "3"}})

	got, err := s.RandomScenes(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}

	// Continue and recent both list the same three ids; the pool must be
	// de-duplicated across sections.
	if len(got) != 3 {
		t.Fatalf("expected the 3 distinct scenes of the index, got %d", len(got))
	}
}
