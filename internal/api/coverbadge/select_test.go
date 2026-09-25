package coverbadge

import (
	"image/color"
	"strings"
	"testing"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
)

func scene(width, height int, tagNames ...string) *library.VideoData {
	sp := &gql.SceneParts{Id: "1"}
	if width > 0 || height > 0 {
		sp.Files = []*gql.ScenePartsFilesVideoFile{{Width: width, Height: height}}
	}
	for _, n := range tagNames {
		sp.Tags = append(sp.Tags, &gql.TagPartsArrayTagsTag{TagParts: gql.TagParts{Id: n, Name: n}})
	}
	return &library.VideoData{SceneParts: sp}
}

var all = config.CoverBadges{Quality: true, Format: true, Passthrough: true}

func labels(bs []Badge) []string {
	out := make([]string, len(bs))
	for i := range bs {
		out[i] = bs[i].Label
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestForScene_QualityTierTags(t *testing.T) {
	on := config.CoverBadges{Quality: true}
	for _, c := range []struct {
		tags []string
		want string
		fill color.RGBA
	}{
		{[]string{"HQ", "8K"}, "8K", Gold},
		{[]string{"7k"}, "7K", Silver},
		{[]string{"6k hbr", "HQ"}, "6K HBR", Bronze},
		// The highest tier wins when a scene carries several.
		{[]string{"6K HBR", "8K", "7K"}, "8K", Gold},
	} {
		got := ForScene(scene(5760, 2880, c.tags...), on, nil)
		if len(got) != 1 || got[0].Label != c.want || got[0].Kind != KindQuality || got[0].Fill != c.fill {
			t.Errorf("tags %v: got %+v, want one %q badge", c.tags, got, c.want)
		}
	}
}

func TestForScene_QualityFallsBackToResolution(t *testing.T) {
	on := config.CoverBadges{Quality: true}
	for _, c := range []struct {
		w, h int
		tags []string
		want string
	}{
		{5400, 2700, nil, "5K"},
		{3840, 1920, []string{"HQ"}, "4K"},
		{1920, 1080, nil, "1080p"},
	} {
		got := ForScene(scene(c.w, c.h, c.tags...), on, nil)
		if len(got) != 1 || got[0].Label != c.want || got[0].Fill != Neutral {
			t.Errorf("%dx%d %v: got %+v, want a neutral %q badge", c.w, c.h, c.tags, got, c.want)
		}
	}
	if got := ForScene(scene(0, 0), on, nil); len(got) != 0 {
		t.Errorf("a scene without files or tier tags gets no quality badge, got %+v", got)
	}
}

func TestForScene_FormatLabels(t *testing.T) {
	rules := config.DefaultVideoRules()
	on := config.CoverBadges{Format: true}
	for _, c := range []struct {
		tags []string
		want []string
	}{
		{[]string{"DOME"}, []string{"180"}},
		{[]string{"SPHERE"}, []string{"360"}},
		{[]string{"FISHEYE"}, []string{"FISHEYE"}},
		{[]string{"MKX200"}, []string{"FISHEYE 200"}},
		{[]string{"RF52"}, []string{"FISHEYE 190"}},
		{[]string{"FLAT", "SBS"}, []string{"FLAT 3D"}},
		{[]string{"3D Conversion"}, []string{"FLAT 3D"}},
		{[]string{"FLAT"}, nil},
		{nil, nil},
	} {
		got := labels(ForScene(scene(5760, 2880, c.tags...), on, rules))
		if !equal(got, c.want) {
			t.Errorf("tags %v: got %v, want %v", c.tags, got, c.want)
		}
	}
}

func TestForScene_Passthrough(t *testing.T) {
	rules := config.DefaultVideoRules()
	on := config.CoverBadges{Passthrough: true}
	for _, c := range []struct {
		tags []string
		want []string
	}{
		{[]string{"Alpha"}, []string{"AR"}},
		{[]string{"Chroma Key"}, []string{"AR"}},
		{[]string{"DOME"}, nil},
	} {
		got := ForScene(scene(5760, 2880, c.tags...), on, rules)
		if !equal(labels(got), c.want) {
			t.Errorf("tags %v: got %v, want %v", c.tags, labels(got), c.want)
		}
		if len(got) == 1 && got[0].Kind != KindPassthrough {
			t.Errorf("expected a passthrough badge, got %+v", got[0])
		}
	}
}

func TestForScene_OrderAndSwitches(t *testing.T) {
	rules := config.DefaultVideoRules()
	vd := scene(8192, 4096, "8K", "DOME", "Alpha")

	if got := labels(ForScene(vd, all, rules)); !equal(got, []string{"8K", "180", "AR"}) {
		t.Fatalf("expected quality, format, passthrough in that order, got %v", got)
	}
	if got := ForScene(vd, config.CoverBadges{}, rules); len(got) != 0 {
		t.Fatalf("all badges off must select none, got %v", labels(got))
	}
	if got := labels(ForScene(vd, config.CoverBadges{Quality: true, Passthrough: true}, rules)); !equal(got, []string{"8K", "AR"}) {
		t.Fatalf("format off must drop the projection, got %v", got)
	}
}

func TestForScene_NilSafe(t *testing.T) {
	if got := ForScene(nil, all, nil); got != nil {
		t.Fatalf("nil scene: got %v", got)
	}
	if got := ForScene(&library.VideoData{}, all, nil); got != nil {
		t.Fatalf("scene without parts: got %v", got)
	}
	vd := scene(0, 0)
	vd.SceneParts.Files = []*gql.ScenePartsFilesVideoFile{nil}
	if got := ForScene(vd, config.CoverBadges{Quality: true}, nil); len(got) != 0 {
		t.Fatalf("nil file: got %v", got)
	}
}

func TestKey_NamesTheBadgeSet(t *testing.T) {
	a := []Badge{{Kind: KindQuality, Label: "8K"}, {Kind: KindPassthrough, Label: "AR"}}
	b := []Badge{{Kind: KindQuality, Label: "7K"}, {Kind: KindPassthrough, Label: "AR"}}
	if Key(a) == Key(b) {
		t.Fatal("different badge sets must have different keys")
	}
	if Key(nil) != "" {
		t.Fatalf("no badges must key as empty, got %q", Key(nil))
	}
}

func TestTierTags(t *testing.T) {
	got := TierTags()
	if strings.Join(got, ",") != "8K,7K,6K HBR" {
		t.Fatalf("expected the tiers best first, got %v", got)
	}
	got[0] = "changed"
	if TierTags()[0] != "8K" {
		t.Fatal("TierTags must return a copy")
	}
}

// timed returns an 8K scene whose primary file has the given duration in
// seconds and frame rate.
func timed(duration, fps float64, tagNames ...string) *library.VideoData {
	vd := scene(8192, 4096, tagNames...)
	vd.SceneParts.Files[0].Duration = duration
	vd.SceneParts.Files[0].Frame_rate = fps
	return vd
}

func TestForScene_Duration(t *testing.T) {
	on := config.CoverBadges{Duration: true}
	for _, c := range []struct {
		seconds float64
		want    []string
	}{
		{2520, []string{"42 min"}},
		{2549, []string{"42 min"}},
		{20, []string{"1 min"}},
		{3599, []string{"1 h 00"}},
		{3900, []string{"1 h 05"}},
		{2*3600 + 30*60, []string{"2 h 30"}},
		{0, nil},
		{-5, nil},
	} {
		got := ForScene(timed(c.seconds, 0), on, nil)
		if !equal(labels(got), c.want) {
			t.Errorf("%v s: got %v, want %v", c.seconds, labels(got), c.want)
		}
		if len(got) == 1 && (got[0].Kind != KindDuration || got[0].Fill != Neutral || got[0].Text != White) {
			t.Errorf("%v s: expected a neutral duration badge, got %+v", c.seconds, got[0])
		}
	}
}

func TestForScene_FrameRate(t *testing.T) {
	on := config.CoverBadges{FrameRate: true}
	for _, c := range []struct {
		fps  float64
		want []string
	}{
		{59.94, []string{"60 fps"}},
		{29.97, []string{"30 fps"}},
		{23.976, []string{"24 fps"}},
		{90, []string{"90 fps"}},
		{0, nil},
	} {
		got := ForScene(timed(60, c.fps), on, nil)
		if !equal(labels(got), c.want) {
			t.Errorf("%v fps: got %v, want %v", c.fps, labels(got), c.want)
		}
		if len(got) == 1 && (got[0].Kind != KindFrameRate || got[0].Fill != Neutral || got[0].Text != White) {
			t.Errorf("%v fps: expected a neutral frame rate badge, got %+v", c.fps, got[0])
		}
	}
}

func TestForScene_DurationAndFrameRateNeedAFile(t *testing.T) {
	on := config.CoverBadges{Duration: true, FrameRate: true}
	if got := ForScene(scene(0, 0), on, nil); len(got) != 0 {
		t.Fatalf("a scene without files gets neither badge, got %v", labels(got))
	}
	vd := scene(0, 0)
	vd.SceneParts.Files = []*gql.ScenePartsFilesVideoFile{nil}
	if got := ForScene(vd, on, nil); len(got) != 0 {
		t.Fatalf("a nil file gets neither badge, got %v", labels(got))
	}
}

func TestForScene_AllFiveInOrder(t *testing.T) {
	rules := config.DefaultVideoRules()
	on := config.CoverBadges{Quality: true, Format: true, Passthrough: true, Duration: true, FrameRate: true}

	got := labels(ForScene(timed(2520, 59.94, "8K", "DOME", "Alpha"), on, rules))

	if !equal(got, []string{"8K", "180", "AR", "42 min", "60 fps"}) {
		t.Fatalf("expected quality, format, AR, duration, frame rate, got %v", got)
	}
}

func TestForScene_LowDetailMutesTheTier(t *testing.T) {
	on := config.CoverBadges{Quality: true}
	for _, c := range []struct {
		tags []string
		want string
		fill color.RGBA
		text color.RGBA
	}{
		{[]string{"8K"}, "8K", Gold, Dark},
		{[]string{"8K", "Low Detail"}, "8K", Slate, White},
		{[]string{"low detail", "7K"}, "7K", Slate, White},
		{[]string{"6K HBR", " LOW DETAIL "}, "6K HBR", Slate, White},
		{[]string{"7K", "Low"}, "7K", Silver, Dark},
	} {
		got := ForScene(scene(8192, 4096, c.tags...), on, nil)
		if len(got) != 1 || got[0].Label != c.want || got[0].Fill != c.fill || got[0].Text != c.text {
			t.Errorf("tags %v: got %+v, want %q in %v on %v", c.tags, got, c.want, c.text, c.fill)
		}
	}
}

func TestForScene_LowDetailMutesTheResolution(t *testing.T) {
	on := config.CoverBadges{Quality: true}

	got := ForScene(scene(5400, 2700, "Low Detail"), on, nil)

	if len(got) != 1 || got[0].Label != "5K" || got[0].Fill != Slate || got[0].Text != White {
		t.Fatalf("expected a slate 5K badge, got %+v", got)
	}
}

func TestSlateIsTheLowDetailColour(t *testing.T) {
	if Slate != (color.RGBA{R: 0x5b, G: 0x65, B: 0x73, A: 0xff}) {
		t.Fatalf("Low Detail is slate #5b6573, got %v", Slate)
	}
}

func TestKey_TellsAMutedTierApart(t *testing.T) {
	on := config.CoverBadges{Quality: true}
	gold := ForScene(scene(8192, 4096, "8K"), on, nil)
	slate := ForScene(scene(8192, 4096, "8K", "Low Detail"), on, nil)
	if Key(gold) == Key(slate) {
		t.Fatal("a gold and a slate 8K must not share a rendered cover")
	}
}
