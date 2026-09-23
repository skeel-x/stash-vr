package library

import (
	"context"
	"testing"
)

func TestLoadTags_RefreshesCacheOnEveryCall(t *testing.T) {
	client := &fakeGraphQL{payloads: []string{
		`{"findTags":{"tags":[{"id":"1","name":"Old","sort_name":"","aliases":[],"parents":[]}]}}`,
		`{"findTags":{"tags":[{"id":"1","name":"New","sort_name":"","aliases":[],"parents":[]},{"id":"2","name":"Added","sort_name":"","aliases":[],"parents":[]}]}}`,
	}}
	svc := NewService(client)

	if err := svc.LoadTags(context.Background()); err != nil {
		t.Fatalf("first LoadTags: %v", err)
	}
	if err := svc.LoadTags(context.Background()); err != nil {
		t.Fatalf("second LoadTags: %v", err)
	}

	if client.calls != 2 {
		t.Fatalf("expected Stash to be queried on every LoadTags call, got %d calls", client.calls)
	}
	if got := svc.tagCache["1"].Name; got != "New" {
		t.Fatalf("expected renamed tag to be visible after reload, got %q", got)
	}
	if _, ok := svc.tagCache["2"]; !ok {
		t.Fatal("expected tag added in Stash to be present after reload")
	}
}

func TestAncestors_SkipsParentMissingFromCache(t *testing.T) {
	svc := NewService(nil)
	svc.tagCache = map[string]*Tag{
		"child": {Id: "child", Name: "child", ParentIds: []string{"missing"}},
	}

	out := svc.ancestors("child")

	if len(out) != 0 {
		t.Fatalf("expected no ancestors for a parent missing from the cache, got %v", out)
	}
}
