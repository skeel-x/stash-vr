package library

import (
	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

// Format is what the video rules resolve to for one scene. Empty strings
// and zero mean "not set"; players then fall back to their own defaults.
type Format struct {
	Projection   string
	Stereo       string
	Lens         string
	Fov          float32
	Passthrough  bool
	ProfileScene string

	// Screen geometry and environment for a generated HereSphere profile;
	// nil and "" mean unset. See config.VideoRule.
	PositionX, PositionY, PositionZ *float64
	Pitch, Yaw, Roll                *float64
	ZoomX, ZoomY, PanX, PanY        *float64
	OriginX, OriginY, OriginZ       *float64
	Background                      string
	BackgroundColor                 string
	Mask                            string
	// EyeSwap and ForceMono are HereSphere format flags; DeoVR maps force
	// mono to its stereo mode off and has nothing for eye swap.
	EyeSwap, ForceMono bool
	// Generated is true when a matching rule sets any geometry, background
	// or mask field, or eye swap or force mono ends up on, so a profile is
	// generated for scenes without one.
	Generated bool
}

// ResolveFormat applies rules in order to the scene's tags: a rule whose
// tag matches a tag name or alias (case-insensitively) sets every field
// it carries, so later rules override earlier ones.
func ResolveFormat(rules []config.VideoRule, tags []*gql.TagPartsArrayTagsTag) Format {
	var f Format
	for i := range rules {
		r := &rules[i]
		if !hasTag(tags, r.Tag) {
			continue
		}
		if r.Projection != "" {
			f.Projection = r.Projection
		}
		if r.Stereo != "" {
			f.Stereo = r.Stereo
		}
		if r.Lens != "" {
			f.Lens = r.Lens
		}
		if r.Fov != 0 {
			f.Fov = r.Fov
		}
		if r.Passthrough {
			f.Passthrough = true
		}
		if r.Profile != "" {
			f.ProfileScene = r.Profile
		}
		if r.EyeSwap != nil {
			f.EyeSwap = *r.EyeSwap
		}
		if r.ForceMono != nil {
			f.ForceMono = *r.ForceMono
		}
		resolveGeometry(&f, r)
	}
	if f.EyeSwap || f.ForceMono {
		f.Generated = true
	}
	return f
}

func resolveGeometry(f *Format, r *config.VideoRule) {
	for _, p := range []struct {
		dst **float64
		src *float64
	}{
		{&f.PositionX, r.PositionX}, {&f.PositionY, r.PositionY}, {&f.PositionZ, r.PositionZ},
		{&f.Pitch, r.Pitch}, {&f.Yaw, r.Yaw}, {&f.Roll, r.Roll},
		{&f.ZoomX, r.ZoomX}, {&f.ZoomY, r.ZoomY}, {&f.PanX, r.PanX}, {&f.PanY, r.PanY},
		{&f.OriginX, r.OriginX}, {&f.OriginY, r.OriginY}, {&f.OriginZ, r.OriginZ},
	} {
		if p.src != nil {
			v := *p.src
			*p.dst = &v
		}
	}
	if r.Background != "" {
		f.Background = r.Background
	}
	if r.BackgroundColor != "" {
		f.BackgroundColor = r.BackgroundColor
	}
	if r.Mask != "" {
		f.Mask = r.Mask
	}
	if r.HasGeometry() {
		f.Generated = true
	}
}

func hasTag(tags []*gql.TagPartsArrayTagsTag, name string) bool {
	for _, t := range tags {
		if t != nil && util.StrSliceEquals(t.Name, t.Aliases, name) {
			return true
		}
	}
	return false
}

// MatchingRules returns the indexes of the rules whose tag the scene has,
// in rule order: the rules ResolveFormat applies.
func MatchingRules(rules []config.VideoRule, tags []*gql.TagPartsArrayTagsTag) []int {
	out := []int{}
	for i := range rules {
		if hasTag(tags, rules[i].Tag) {
			out = append(out, i)
		}
	}
	return out
}
