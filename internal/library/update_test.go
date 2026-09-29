package library

import (
	"context"
	"errors"
	"testing"
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
