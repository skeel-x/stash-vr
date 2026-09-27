package library

import (
	"encoding/json"
	"math"
	"testing"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

func offsetScene(offset any, tagNames ...string) *VideoData {
	sp := &gql.SceneParts{Id: "7"}
	if offset != nil {
		sp.Custom_fields = map[string]any{VerticalOffsetField: offset}
	}
	sp.Tags = tags(tagNames...)
	return &VideoData{SceneParts: sp}
}

func TestVerticalOffset_ParsesNumbersAndNumericStrings(t *testing.T) {
	cases := []struct {
		name    string
		raw     any
		want    float64
		present bool
		ok      bool
	}{
		{"float64", 0.62, 0.62, true, true},
		{"negative", -1.1, -1.1, true, true},
		{"float32", float32(0.5), 0.5, true, true},
		{"int", 1, 1, true, true},
		{"int64", int64(-1), -1, true, true},
		{"json number", json.Number("0.75"), 0.75, true, true},
		{"bad json number", json.Number("x"), 0, true, false},
		{"string", " -0.42 ", -0.42, true, true},
		{"empty string", "", 0, true, false},
		{"garbage string", "left", 0, true, false},
		{"nan string", "NaN", 0, true, false},
		{"inf string", "+Inf", 0, true, false},
		{"nan", math.NaN(), 0, true, false},
		{"inf", math.Inf(-1), 0, true, false},
		{"bool", true, 0, true, false},
		{"map", map[string]any{"v": 1.0}, 0, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vd := &VideoData{SceneParts: &gql.SceneParts{Custom_fields: map[string]any{VerticalOffsetField: c.raw}}}
			got, present, ok := VerticalOffset(vd)
			if present != c.present || ok != c.ok || math.Abs(got-c.want) > 1e-6 {
				t.Fatalf("got %v %v %v, want %v %v %v", got, present, ok, c.want, c.present, c.ok)
			}
		})
	}
}

func TestVerticalOffset_Missing(t *testing.T) {
	for name, vd := range map[string]*VideoData{
		"nil scene":     nil,
		"nil parts":     {},
		"nil fields":    {SceneParts: &gql.SceneParts{}},
		"other fields":  {SceneParts: &gql.SceneParts{Custom_fields: map[string]any{"other": 1.0}}},
		"explicit null": {SceneParts: &gql.SceneParts{Custom_fields: map[string]any{VerticalOffsetField: nil}}},
	} {
		_, present, ok := VerticalOffset(vd)
		if ok || (present && name != "explicit null") {
			t.Fatalf("%s: got present %v ok %v", name, present, ok)
		}
	}
}

func TestCorrectVertical_RangeAndSign(t *testing.T) {
	cases := []struct {
		offset  float64
		applied bool
		reason  string
	}{
		{0, false, VerticalTooSmall},
		{0.24, false, VerticalTooSmall},
		{-0.24, false, VerticalTooSmall},
		{0.25, true, VerticalApplied},
		{-0.25, true, VerticalApplied},
		{0.62, true, VerticalApplied},
		{-1.1, true, VerticalApplied},
		{1.5, true, VerticalApplied},
		{-1.5, true, VerticalApplied},
		{1.51, false, VerticalTooLarge},
		{-3, false, VerticalTooLarge},
	}
	for _, c := range cases {
		f := Format{Stereo: "sbs"}
		got := CorrectVertical(&f, offsetScene(c.offset), true)
		if got.Applied != c.applied || got.Reason != c.reason || !got.Measured || got.Offset != c.offset {
			t.Fatalf("%v: got %+v", c.offset, got)
		}
		if c.applied {
			if f.Pitch == nil || *f.Pitch != c.offset || !f.Generated {
				t.Fatalf("%v: pitch not set with sign or not generated: %+v", c.offset, f)
			}
		} else if f.Pitch != nil || f.Generated {
			t.Fatalf("%v: format changed outside the range: %+v", c.offset, f)
		}
	}
}

func TestCorrectVertical_Skips(t *testing.T) {
	pitch := 2.0
	cases := []struct {
		name   string
		f      Format
		offset any
		on     bool
		reason string
	}{
		{"not measured", Format{Stereo: "sbs"}, nil, true, VerticalNotMeasured},
		{"invalid", Format{Stereo: "sbs"}, "abc", true, VerticalInvalid},
		{"setting off", Format{Stereo: "sbs"}, 0.8, false, VerticalOff},
		{"mono", Format{Stereo: "mono"}, 0.8, true, VerticalNotStereo},
		{"stereo unknown", Format{Projection: "perspective"}, 0.8, true, VerticalNotStereo},
		{"forced mono", Format{Stereo: "tb", ForceMono: true}, 0.8, true, VerticalNotStereo},
		{"rule pitch", Format{Stereo: "tb", Pitch: &pitch}, 0.8, true, VerticalRulePitch},
	}
	for _, c := range cases {
		f := c.f
		got := CorrectVertical(&f, offsetScene(c.offset), c.on)
		if got.Applied || got.Reason != c.reason {
			t.Fatalf("%s: got %+v", c.name, got)
		}
		if f.Generated {
			t.Fatalf("%s: profile marked generated", c.name)
		}
		if c.name == "rule pitch" && *f.Pitch != 2 {
			t.Fatalf("rule pitch overwritten: %v", *f.Pitch)
		}
		if c.name != "rule pitch" && f.Pitch != nil {
			t.Fatalf("%s: pitch set", c.name)
		}
	}
}

func TestSceneFormat_TBAndRules(t *testing.T) {
	rules := config.DefaultVideoRules()
	f, c := SceneFormat(rules, offsetScene("-0.9", "TB", "DOME"), true)
	if !c.Applied || f.Stereo != "tb" || f.Pitch == nil || *f.Pitch != -0.9 || !f.Generated {
		t.Fatalf("got %+v %+v", f, c)
	}
	f, c = SceneFormat(rules, offsetScene("-0.9", "TB", "DOME"), false)
	if c.Applied || c.Reason != VerticalOff || f.Pitch != nil || f.Generated {
		t.Fatalf("setting off changed the format: %+v %+v", f, c)
	}
	f, c = SceneFormat(rules, offsetScene(0.9, "FLAT"), true)
	if c.Reason != VerticalNotStereo || f.Pitch != nil {
		t.Fatalf("flat scene corrected: %+v %+v", f, c)
	}
	// ResolveFormat, which the cover badges and other players use, is
	// untouched by the offset.
	if g := ResolveFormat(rules, offsetScene(0.9, "DOME").SceneParts.Tags); g.Pitch != nil || g.Generated {
		t.Fatalf("ResolveFormat picked up the offset: %+v", g)
	}
}

func TestSceneFormat_PrecedenceUnchanged(t *testing.T) {
	f, _ := SceneFormat(config.DefaultVideoRules(), offsetScene(0.8, "DOME"), true)
	has := func(id string) bool { return id == "7" || id == "3" }
	if src, _ := ProfileSourceFor("7", f, has, nil); src != ProfileOwn {
		t.Fatalf("own profile should win, got %s", src)
	}
	learned := func() string { return "3" }
	if src, scene := ProfileSourceFor("8", f, has, learned); src != ProfileStudio || scene != "3" {
		t.Fatalf("studio profile should win, got %s %s", src, scene)
	}
	f.ProfileScene = "3"
	if src, _ := ProfileSourceFor("8", f, has, nil); src != ProfileRule {
		t.Fatalf("rule profile should win, got %s", src)
	}
	f.ProfileScene = ""
	if src, scene := ProfileSourceFor("8", f, has, nil); src != ProfileGenerated || scene != "8" {
		t.Fatalf("expected generated, got %s %s", src, scene)
	}
}
