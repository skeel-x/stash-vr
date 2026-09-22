package heresphere

import (
	"context"
	"testing"

	"stash-vr/internal/util"
)

func markerNames(in incomingTags) []string {
	out := make([]string, len(in.markers))
	for i, m := range in.markers {
		out[i] = m.PrimaryTagName + "=" + m.MarkerId
	}
	return out
}

func TestClassify_UntimedUnknownTagIsIgnored(t *testing.T) {
	in := classifyIncomingTags(context.Background(), []tagDto{{Name: "Something:else"}})

	if len(in.markers) != 0 || len(in.tags) != 0 {
		t.Fatalf("untimed unknown tag must create nothing, got %+v", in)
	}
}

func TestClassify_LegacyLegendIsIgnoredEvenWhenTimed(t *testing.T) {
	end := 5000.0
	in := classifyIncomingTags(context.Background(), []tagDto{
		{Name: "P:3", Start: 100, End: &end},
		{Name: "movie:Some Movie", Start: 100, End: &end},
		{Name: "Org:true", Start: 0.1, End: &end},
	})

	if len(in.markers) != 0 {
		t.Fatalf("legacy legends must never become markers, got %v", markerNames(in))
	}
}

func TestClassify_TimedUnknownTagBecomesNewMarker(t *testing.T) {
	end := 30000.0
	in := classifyIncomingTags(context.Background(), []tagDto{{Name: "Kiss:opening", Start: 12000, End: &end}})

	if len(in.markers) != 1 {
		t.Fatalf("expected one marker, got %v", markerNames(in))
	}
	m := in.markers[0]
	if m.PrimaryTagName != "Kiss" || m.Title != "opening" || m.MarkerId != "0" || m.StartSecond != 12 || m.EndSecond == nil || *m.EndSecond != 30 {
		t.Fatalf("unexpected marker %+v", m)
	}
}

func TestClassify_ExistingMarkerKeepsItsId(t *testing.T) {
	in := classifyIncomingTags(context.Background(), []tagDto{{Name: "Kiss", Start: 12000, Rating: util.Ptr(float32(42))}})

	if len(in.markers) != 1 || in.markers[0].MarkerId != "42" {
		t.Fatalf("expected marker id 42, got %v", markerNames(in))
	}
}

func TestClassify_ExistingMarkerAtZeroWithoutEndIsKept(t *testing.T) {
	// A marker at second 0 with no end comes back as Start 0, End nil; the id
	// in Rating must keep it, otherwise UpdateMarkers would destroy it.
	in := classifyIncomingTags(context.Background(), []tagDto{{Name: "Intro", Start: 0, Rating: util.Ptr(float32(7))}})

	if len(in.markers) != 1 || in.markers[0].MarkerId != "7" || in.markers[0].StartSecond != 0 {
		t.Fatalf("expected marker id 7 at 0s, got %v", markerNames(in))
	}
}

func TestClassify_HiddenAndKnownLegendsAreNotMarkers(t *testing.T) {
	minus := -1.0
	in := classifyIncomingTags(context.Background(), []tagDto{
		{Name: "@:Some Performer", Start: -1, End: &minus},
		{Name: "Played:3"},
		{Name: "#:newtag"},
		{Name: "/o"},
	})

	if len(in.markers) != 0 {
		t.Fatalf("expected no markers, got %v", markerNames(in))
	}
	if !in.hasPlayCount || len(in.tags) != 1 || in.tags[0] != "newtag" || len(in.commands) != 1 || in.commands[0] != "/o" {
		t.Fatalf("classification wrong: %+v", in)
	}
}
