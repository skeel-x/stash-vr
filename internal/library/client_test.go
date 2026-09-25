package library

import (
	"context"
	"testing"
)

const sceneA = `{"findScenes":{"scenes":[{"id":"1","title":"A","created_at":"2024-01-01T00:00:00Z","files":[],"tags":[]}]}}`
const sceneB = `{"findScenes":{"scenes":[{"id":"1","title":"B","created_at":"2024-01-01T00:00:00Z","files":[],"tags":[]}]}}`

func TestSetStashClient_DropsCachedScenesAndUsesNewClient(t *testing.T) {
	oldClient := &fakeGraphQL{payloads: []string{sceneA}}
	svc := NewService(oldClient)

	vd, err := svc.GetScene(context.Background(), "1", false)
	if err != nil || vd.Title() != "A" {
		t.Fatalf("expected scene A from old client, got %v %v", vd, err)
	}

	newClient := &fakeGraphQL{payloads: []string{sceneB}}
	svc.SetStashClient(newClient)

	if svc.Client() != newClient {
		t.Fatal("expected Client() to return the new client")
	}
	vd, err = svc.GetScene(context.Background(), "1", false)
	if err != nil || vd.Title() != "B" {
		t.Fatalf("expected scene B refetched from new client, got %v %v", vd, err)
	}
	if oldClient.calls != 1 {
		t.Fatalf("old client must not be used after swap, calls=%d", oldClient.calls)
	}
}

func TestResetCaches_RefetchesScene(t *testing.T) {
	client := &fakeGraphQL{payloads: []string{sceneA, sceneB}}
	svc := NewService(client)

	if _, err := svc.GetScene(context.Background(), "1", false); err != nil {
		t.Fatal(err)
	}
	svc.ResetCaches()
	vd, err := svc.GetScene(context.Background(), "1", false)

	if err != nil || vd.Title() != "B" {
		t.Fatalf("expected refetch after reset, got %v %v", vd, err)
	}
}

func TestResetCaches_RunsResetHooks(t *testing.T) {
	svc := NewService(&fakeGraphQL{})
	calls := 0
	svc.OnReset(func() { calls++ })
	svc.OnReset(func() { calls += 10 })

	svc.ResetCaches()
	svc.SetStashClient(&fakeGraphQL{})

	if calls != 22 {
		t.Fatalf("expected both hooks on every reset, got %d", calls)
	}
}
