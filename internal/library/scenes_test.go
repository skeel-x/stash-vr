package library

import (
	"context"
	"errors"
	"testing"
)

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
