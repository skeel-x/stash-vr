package library

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

// fakeGraphQL answers each MakeRequest with the next JSON payload in order;
// a call whose index is in errs fails with that error instead.
type fakeGraphQL struct {
	payloads []string
	errs     map[int]error
	calls    int
}

func (f *fakeGraphQL) MakeRequest(_ context.Context, _ *graphql.Request, resp *graphql.Response) error {
	n := f.calls
	f.calls++
	if err := f.errs[n]; err != nil {
		return err
	}
	return json.Unmarshal([]byte(f.payloads[n]), resp.Data)
}

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

func TestGetBrowseTags_ReadsTheCacheAndLoadsOnlyWhenEmpty(t *testing.T) {
	loadConfig(t, nil)
	client := &fakeGraphQL{payloads: []string{
		`{"findTags":{"tags":[{"id":"1","name":"Bunny","sort_name":"","aliases":[],"parents":[]},{"id":"2","name":"Hidden","sort_name":"hidden","aliases":[],"parents":[]},{"id":"3","name":"Apple","sort_name":"","aliases":[],"parents":[]}]}}`,
		`{"findTags":{"tags":[{"id":"4","name":"After reset","sort_name":"","aliases":[],"parents":[]}]}}`,
	}}
	svc := NewService(client)

	first, err := svc.GetBrowseTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Name != "Apple" || first[1].Name != "Bunny" {
		t.Fatalf("expected Apple and Bunny by sort name without the excluded tag, got %v", first)
	}
	if _, err := svc.GetBrowseTags(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 {
		t.Fatalf("a filled cache must not query Stash again, got %d calls", client.calls)
	}

	svc.ResetCaches()
	after, err := svc.GetBrowseTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || len(after) != 1 || after[0].Name != "After reset" {
		t.Fatalf("an emptied cache must be loaded again, got %d calls and %v", client.calls, after)
	}
}

func TestGetBrowseTags_LoadFailure(t *testing.T) {
	svc := NewService(&fakeGraphQL{errs: map[int]error{0: errors.New("stash unreachable")}})

	if _, err := svc.GetBrowseTags(context.Background()); err == nil {
		t.Fatal("expected the load failure while there is no cache")
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
