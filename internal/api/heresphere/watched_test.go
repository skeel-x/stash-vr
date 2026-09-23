package heresphere

import (
	"slices"
	"testing"

	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestFormatResume(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0, "0:00"},
		{754, "12:34"},
		{3723, "1:02:03"},
		{59.9, "0:59"},
	}
	for _, c := range cases {
		if got := formatResume(c.seconds); got != c.want {
			t.Errorf("formatResume(%v) = %q want %q", c.seconds, got, c.want)
		}
	}
}

func TestWatchedTags_FreshScene(t *testing.T) {
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "1"}}
	got := names(watchedTags(vd))
	if !slices.Equal(got, []string{"Watched:no"}) {
		t.Fatalf("expected only Watched:no for a fresh scene, got %v", got)
	}
}

func TestWatchedTags_PlayedWithResume(t *testing.T) {
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "1", Play_count: util.Ptr(2), Resume_time: util.Ptr(754.0)}}
	got := names(watchedTags(vd))
	if !slices.Equal(got, []string{"Watched:yes", "Resume:12:34"}) {
		t.Fatalf("expected Watched:yes and Resume:12:34, got %v", got)
	}
}

func TestWatchedTags_ShortResumeIsIgnored(t *testing.T) {
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "1", Play_count: util.Ptr(1), Resume_time: util.Ptr(4.9)}}
	got := names(watchedTags(vd))
	if !slices.Equal(got, []string{"Watched:yes"}) {
		t.Fatalf("expected only Watched:yes when resume_time < 5, got %v", got)
	}
}

func TestGetTags_IncludesWatchedAndResume(t *testing.T) {
	loadDefaultRules(t)
	vd := &library.VideoData{SceneParts: &gql.SceneParts{Id: "9",
		Files:       []*gql.ScenePartsFilesVideoFile{{Basename: "nine.mp4", Duration: 100, Height: 1080}},
		Play_count:  util.Ptr(3),
		Resume_time: util.Ptr(3723.0),
	}}
	got := names(getTags(vd))
	for _, want := range []string{"Played:3", "Watched:yes", "Resume:1:02:03"} {
		if !slices.Contains(got, want) {
			t.Fatalf("expected %q in tags, got %v", want, got)
		}
	}
}
