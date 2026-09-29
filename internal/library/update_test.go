package library

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestUpdateFavorite_UnknownSceneReturnsErrSceneNotFound(t *testing.T) {
	loadConfig(t, nil)
	client := &fakeGraphQL{payloads: []string{
		`{"findTags":{"tags":[{"id":"5","name":"FAVORITE"}]}}`,
		`{"findScene":null}`,
	}}
	svc := NewService(client)

	err := svc.UpdateFavorite(context.Background(), "999999", true)

	if !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("expected ErrSceneNotFound, got %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("no tag update may follow a missing scene, calls=%d", client.calls)
	}
}

func TestUpdateFavorite_AddsAndRemovesTag(t *testing.T) {
	loadConfig(t, nil)
	client := &fakeGraphQL{payloads: []string{
		`{"findTags":{"tags":[{"id":"5","name":"FAVORITE"}]}}`,
		`{"findScene":{"tags":[{"id":"1","name":"Other","sort_name":"","aliases":[],"parents":[]}]}}`,
		`{"sceneUpdate":{"id":"7"}}`,
	}}
	svc := NewService(client)

	if err := svc.UpdateFavorite(context.Background(), "7", true); err != nil {
		t.Fatal(err)
	}
	if client.calls != 3 {
		t.Fatalf("expected find tag, find scene and update, got %d calls", client.calls)
	}
}

func TestDelete_EvictsSceneResetsSectionsAndLogsIt(t *testing.T) {
	loadConfig(t, nil)
	client := &fakeGraphQL{payloads: []string{
		`{"findScenes":{"scenes":[{"id":"7","title":"Seven","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s7.mp4","duration":100,"path":"/s7.mp4","height":1080}],"tags":[]}]}}`,
		`{"sceneDestroy":true}`,
	}}
	svc := NewService(client)
	if _, err := svc.GetScene(context.Background(), "7", false); err != nil {
		t.Fatal(err)
	}
	if len(svc.SearchCachedScenes("Seven", 1)) != 1 {
		t.Fatal("expected the scene cached")
	}
	svc.muSets.Lock()
	gen := svc.setsGen
	svc.muSets.Unlock()
	var logged bytes.Buffer
	ctx := zerolog.New(&logged).WithContext(context.Background())

	if err := svc.Delete(ctx, "7"); err != nil {
		t.Fatal(err)
	}

	if client.calls != 2 {
		t.Fatalf("expected the fetch and the destroy only, got %d calls", client.calls)
	}
	if len(svc.SearchCachedScenes("Seven", 1)) != 0 {
		t.Fatal("the deleted scene must leave the cache")
	}
	svc.muVdCache.RLock()
	_, cached := svc.vdCache["7"]
	svc.muVdCache.RUnlock()
	if cached {
		t.Fatal("the deleted scene must not stay as a placeholder either")
	}
	svc.muSets.Lock()
	after := svc.setsGen
	svc.muSets.Unlock()
	if after != gen+1 {
		t.Fatalf("expected the sections reset once, generation %d -> %d", gen, after)
	}
	line := logged.String()
	for _, want := range []string{`"level":"info"`, `"id":"7"`, `"title":"Seven"`, `"message":"Deleted scene"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %s: %s", want, line)
		}
	}
}

func TestDelete_FailureLeavesSceneCached(t *testing.T) {
	loadConfig(t, nil)
	client := &fakeGraphQL{payloads: []string{
		`{"findScenes":{"scenes":[{"id":"7","title":"Seven","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"s7.mp4","duration":100,"path":"/s7.mp4","height":1080}],"tags":[]}]}}`,
	}, errs: map[int]error{1: errors.New("boom")}}
	svc := NewService(client)
	if _, err := svc.GetScene(context.Background(), "7", false); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(context.Background(), "7")

	if err == nil || !strings.Contains(err.Error(), "SceneDestroy: boom") {
		t.Fatalf("expected the destroy error, got %v", err)
	}
	if len(svc.SearchCachedScenes("Seven", 1)) != 1 {
		t.Fatal("a failed delete must keep the scene cached")
	}
}
