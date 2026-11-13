package library

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestPickablePerformers_MergesStarredAndPickedSorted(t *testing.T) {
	client := &fakeGraphQL{payloads: []string{
		`{"findPerformers":{"performers":[{"id":"2","name":"zoe","scene_count":4,"favorite":true},{"id":"1","name":"Alice","scene_count":9,"favorite":true}]}}`,
		// Picks the starred list lacks are looked up one by one; a performer
		// deleted in Stash answers null and is left out.
		`{"findPerformer":{"id":"7","name":"Mia","scene_count":1,"favorite":false}}`,
		`{"findPerformer":null}`,
	}}
	got, err := NewService(client).PickablePerformers(context.Background(), []string{"1", "7", "999"})
	if err != nil {
		t.Fatal(err)
	}
	want := "[{1 Alice 9 true false} {7 Mia 1 false false} {2 zoe 4 true false}]"
	if fmt.Sprint(got) != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
	if client.calls != 3 {
		t.Fatalf("expected the starred query and one lookup per missing pick, got %d calls", client.calls)
	}
}

func TestPickablePerformers_LookupErrorIsReported(t *testing.T) {
	client := &fakeGraphQL{payloads: []string{
		`{"findPerformers":{"performers":[{"id":"1","name":"Alice","scene_count":9,"favorite":true}]}}`,
		``,
	}, errs: map[int]error{1: errors.New("dial tcp: refused")}}
	_, err := NewService(client).PickablePerformers(context.Background(), []string{"7"})
	if err == nil || err.Error() != "FindPickablePerformer 7: dial tcp: refused" {
		t.Fatalf("err = %v", err)
	}
}

func TestPickablePerformers_AsksForIdsOnlyWhenSomeAreMissing(t *testing.T) {
	client := &fakeGraphQL{payloads: []string{
		`{"findPerformers":{"performers":[{"id":"1","name":"Alice","scene_count":9,"favorite":true}]}}`,
	}}
	got, err := NewService(client).PickablePerformers(context.Background(), []string{"1"})
	if err != nil || len(got) != 1 || client.calls != 1 {
		t.Fatalf("got %v, %v after %d calls", got, err, client.calls)
	}
	client = &fakeGraphQL{payloads: []string{`{"findPerformers":{"performers":[]}}`}}
	got, err = NewService(client).PickablePerformers(context.Background(), nil)
	if err != nil || len(got) != 0 || client.calls != 1 {
		t.Fatalf("no starred, no picks: got %v, %v after %d calls", got, err, client.calls)
	}
}

func TestUnknownPerformers_KeepsThePicksTicked(t *testing.T) {
	got := UnknownPerformers([]string{"72", "515"})
	if fmt.Sprint(got) != "[{72 Performer 72 0 false true} {515 Performer 515 0 false true}]" {
		t.Fatalf("got %v", got)
	}
}
