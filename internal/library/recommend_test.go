package library

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"stash-vr/internal/stash/gql"
)

type recTag = gql.FindRecommendationScenesFindScenesFindScenesResultTypeScenesSceneTagsTag
type recPerformer = gql.FindRecommendationScenesFindScenesFindScenesResultTypeScenesScenePerformersPerformer
type recStudio = gql.FindRecommendationScenesFindScenesFindScenesResultTypeScenesSceneStudio
type recFile = gql.FindRecommendationScenesFindScenesFindScenesResultTypeScenesSceneFilesVideoFile

var recNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func daysAgo(d float64) *time.Time {
	t := recNow.Add(-time.Duration(d * 24 * float64(time.Hour)))
	return &t
}

func intp(v int) *int { return &v }

type recOpt func(*recScene)

// rs builds a scene with one 2D file unless an option changes the files.
func rs(id string, opts ...recOpt) *recScene {
	s := &recScene{Id: id, Files: []*recFile{{Width: 1920, Height: 1080}}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// tag adds a tag whose id is its name.
func tag(names ...string) recOpt {
	return func(s *recScene) {
		for _, n := range names {
			s.Tags = append(s.Tags, &recTag{Id: n, Name: n})
		}
	}
}

func sortTag(id, sortName string) recOpt {
	return func(s *recScene) {
		s.Tags = append(s.Tags, &recTag{Id: id, Name: id, Sort_name: &sortName})
	}
}

func perf(ids ...string) recOpt {
	return func(s *recScene) {
		for _, id := range ids {
			s.Performers = append(s.Performers, &recPerformer{Id: id})
		}
	}
}

func studio(id string) recOpt { return func(s *recScene) { s.Studio = &recStudio{Id: id} } }

// watched marks the scene played to the end d days ago.
func watched(d float64) recOpt {
	return func(s *recScene) { s.Play_count = intp(1); s.Last_played_at = daysAgo(d) }
}

func resumed(sec float64) recOpt { return func(s *recScene) { s.Resume_time = &sec } }
func rated(r int) recOpt         { return func(s *recScene) { s.Rating100 = intp(r) } }
func ocount(n int) recOpt        { return func(s *recScene) { s.O_counter = intp(n) } }
func lastPlayed(d float64) recOpt {
	return func(s *recScene) { s.Last_played_at = daysAgo(d) }
}
func files(dims ...[2]int) recOpt {
	return func(s *recScene) {
		s.Files = nil
		for _, d := range dims {
			s.Files = append(s.Files, &recFile{Width: d[0], Height: d[1]})
		}
	}
}

// fillers returns n unplayed scenes with tags of their own, so the shared
// features in a test are rare enough to carry weight.
func fillers(n int) []*recScene {
	out := make([]*recScene, n)
	for i := range out {
		out[i] = rs(fmt.Sprintf("f%d", i), tag(fmt.Sprintf("ft%d", i)))
	}
	return out
}

func recommendIDs(scenes []*recScene, size int) []string {
	return recommend(scenes, "hidden", recNow, size)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestHistoryWeight(t *testing.T) {
	cases := []struct {
		name string
		s    *recScene
		want float64
	}{
		{"played today, finished", rs("1", watched(0)), 2},
		{"played today, stopped midway", rs("1", watched(0), resumed(600)), 1},
		{"a few seconds of resume still counts as finished", rs("1", watched(0), resumed(3)), 2},
		{"resumed but never finished", rs("1", resumed(600), lastPlayed(0)), 1},
		{"o-count adds, capped at 3", rs("1", watched(0), ocount(7)), 5},
		{"o-count below the cap", rs("1", watched(0), ocount(2)), 4},
		{"rating 100 adds 2", rs("1", watched(0), rated(100)), 4},
		{"rating 70 adds half", rs("1", watched(0), rated(70)), 2.5},
		{"rating 60 adds nothing", rs("1", watched(0), rated(60)), 2},
		{"30 days halves", rs("1", watched(30)), 1},
		{"60 days quarters", rs("1", watched(60)), 0.5},
		{"no last played: 60 day age", rs("1", rated(80)), (1 + 1) * 0.25},
		{"played in the future counts as today", rs("1", watched(-5)), 2},
	}
	for _, c := range cases {
		if got := historyWeight(c.s, recNow); !near(got, c.want) {
			t.Errorf("%s: weight = %v, want %v", c.name, got, c.want)
		}
	}
}

func historyIDs(scenes []*recScene) []string {
	h := pickHistory(scenes, recNow)
	out := make([]string, len(h))
	for i, e := range h {
		out[i] = e.scene.Id
	}
	slices.Sort(out)
	return out
}

func TestPickHistory_WindowAndFallback(t *testing.T) {
	var scenes []*recScene
	for i := 0; i < 10; i++ {
		scenes = append(scenes, rs(fmt.Sprintf("r%d", i), watched(float64(i*5))))
	}
	scenes = append(scenes,
		rs("old", watched(200)),                    // played outside the window
		rs("oldrated", rated(90), lastPlayed(200)), // rated, last played outside
		rs("rated", rated(80)),                     // rated, never played: 60 day age, in the window
		rs("loved", ocount(1), lastPlayed(10)),     // o-count inside the window
		rs("meh", rated(70)),                       // below the rating bar, never played
		rs("resume", resumed(30), lastPlayed(2)),   // resume counts as played
		rs("never"),
	)

	got := historyIDs(scenes)
	want := []string{"loved", "r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "rated", "resume"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("history = %v, want %v", got, want)
	}

	// With fewer than 10 scenes played in the window, fall back to all time.
	scenes = slices.DeleteFunc(scenes, func(s *recScene) bool { return s.Id == "r0" || s.Id == "r1" })
	got = historyIDs(scenes)
	want = []string{"loved", "old", "oldrated", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "rated", "resume"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("fallback history = %v, want %v", got, want)
	}
}

func TestRecommend_EmptyHistory(t *testing.T) {
	scenes := append(fillers(5), rs("a", tag("x")), rs("b", tag("x")))
	if got := recommendIDs(scenes, 50); len(got) != 0 {
		t.Fatalf("expected nothing without a history, got %v", got)
	}
}

func TestRecommend_PerformerOutweighsTagAndStudio(t *testing.T) {
	scenes := append([]*recScene{
		rs("h", watched(0), perf("p1"), tag("t1"), studio("s1")),
		rs("byTag", tag("t1")),
		rs("byPerformer", perf("p1")),
		rs("byStudio", studio("s1")),
		rs("unrelated", tag("zz")),
	}, fillers(20)...)

	got := recommendIDs(scenes, 50)

	// Same document frequency, so only the feature weight differs; tag and
	// studio tie and keep the query order. Unrelated scenes score 0 and are left out.
	want := []string{"byPerformer", "byTag", "byStudio"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("recommended = %v, want %v", got, want)
	}
}

func TestRecommend_RareFeaturesWeighMore(t *testing.T) {
	scenes := []*recScene{rs("h", watched(0), tag("common", "rare"))}
	scenes = append(scenes, rs("byCommon", tag("common")), rs("byRare", tag("rare")))
	for i := 0; i < 10; i++ {
		scenes = append(scenes, rs(fmt.Sprintf("c%d", i), tag("common"), watched(400)))
	}
	scenes = append(scenes, fillers(20)...)

	got := recommendIDs(scenes, 50)

	if len(got) != 2 || got[0] != "byRare" || got[1] != "byCommon" {
		t.Fatalf("expected the rare tag to rank first, got %v", got)
	}
}

func TestIdf(t *testing.T) {
	if got := idf(100, 9); !near(got, math.Log(10)) {
		t.Fatalf("idf(100, 9) = %v", got)
	}
	// A feature on (almost) every scene says nothing: never negative.
	if got := idf(10, 10); got != 0 {
		t.Fatalf("idf(10, 10) = %v, want 0", got)
	}
}

func TestRecommend_IgnoresExcludedAndFormatTags(t *testing.T) {
	excluded := []string{"DOME", "SPHERE", "FISHEYE", "FLAT", "RF52", "MKX200", "MKX220", "VRCA220",
		"SBS", "TB", "MONO", "RL", "Alpha", "Chroma Key", "8K", "7K", "6K HBR", "HQ", "VRP:Studio X", "vrp:lower", "chroma key", "8k"}
	for _, name := range excluded {
		scenes := append([]*recScene{
			rs("h", watched(0), tag(name)),
			rs("c", tag(name)),
		}, fillers(20)...)
		if got := recommendIDs(scenes, 50); len(got) != 0 {
			t.Errorf("tag %q must not be a feature, got %v", name, got)
		}
	}

	scenes := append([]*recScene{
		rs("h", watched(0), sortTag("secret", "hidden")),
		rs("c", sortTag("secret", "hidden")),
	}, fillers(20)...)
	if got := recommendIDs(scenes, 50); len(got) != 0 {
		t.Errorf("tags with the excluded sort name must not be features, got %v", got)
	}
}

func TestRecommend_NormalisesByFeatureCount(t *testing.T) {
	scenes := append([]*recScene{
		rs("h", watched(0), tag("t1")),
		rs("heavy", tag("t1", "a", "b", "c")),
		rs("lean", tag("t1")),
	}, fillers(20)...)

	got := recommendIDs(scenes, 50)

	if fmt.Sprint(got) != "[lean heavy]" {
		t.Fatalf("expected the lean scene first, got %v", got)
	}
}

func TestRecommend_OnlyUnplayedScenesWithFiles(t *testing.T) {
	scenes := append([]*recScene{
		rs("h", watched(0), perf("p1")),
		rs("playedLongAgo", watched(300), perf("p1")),
		rs("started", resumed(20), perf("p1")),
		rs("ratedUnplayed", rated(90), perf("p1")),
		rs("noFile", perf("p1"), files()),
		rs("zeroCounts", perf("p1"), func(s *recScene) { s.Play_count = intp(0); s.Resume_time = new(float64) }),
		rs("fresh", perf("p1")),
	}, fillers(20)...)

	got := recommendIDs(scenes, 50)

	if fmt.Sprint(got) != "[zeroCounts fresh]" {
		t.Fatalf("recommended = %v", got)
	}
}

func TestRecommend_MatchesFormatClassOfHistory(t *testing.T) {
	vrFile := files([2]int{3840, 1920})
	squareVR := files([2]int{5760, 5760})
	base := func(historyVR bool) []*recScene {
		h1 := rs("h1", watched(0), perf("p1"))
		if historyVR {
			h1 = rs("h1", watched(0), perf("p1"), tag("dome"))
		}
		return append([]*recScene{
			h1,
			rs("h2", watched(0), perf("p1"), ocount(3)), // flat, heavier
			rs("vrTag", perf("p1"), tag("SPHERE")),
			rs("vrFile", perf("p1"), vrFile),
			rs("vrSquare", perf("p1"), squareVR),
			rs("wide4k", perf("p1"), files([2]int{3840, 2160})),
			rs("flat", perf("p1")),
		}, fillers(20)...)
	}

	// h2 carries more weight than h1, so the history is mostly flat.
	if got := recommendIDs(base(true), 50); fmt.Sprint(got) != "[wide4k flat]" {
		t.Fatalf("flat history: %v", got)
	}

	vr := base(true)
	vr[1] = rs("h2", watched(0), perf("p1"), ocount(3), vrFile)
	if got := recommendIDs(vr, 50); fmt.Sprint(got) != "[vrTag vrFile vrSquare]" {
		t.Fatalf("VR history: %v", got)
	}
}

func TestRecommend_SizeAndTiesKeepQueryOrder(t *testing.T) {
	scenes := append([]*recScene{
		rs("h", watched(0), perf("p1")),
		rs("newest", perf("p1")),
		rs("middle", perf("p1")),
		rs("oldest", perf("p1")),
	}, fillers(20)...)

	if got := recommendIDs(scenes, 2); fmt.Sprint(got) != "[newest middle]" {
		t.Fatalf("recommended = %v", got)
	}
}

func TestRecommend_SkipsNilEntries(t *testing.T) {
	withNils := func(s *recScene) {
		s.Tags = append(s.Tags, nil)
		s.Performers = append(s.Performers, nil)
		s.Files = append(s.Files, nil)
	}
	scenes := append([]*recScene{nil, rs("h", watched(0), perf("p1"), withNils), rs("c", perf("p1"), withNils)}, fillers(20)...)
	if got := recommendIDs(scenes, 50); fmt.Sprint(got) != "[c]" {
		t.Fatalf("recommended = %v", got)
	}
}
