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
}

// ResolveFormat applies rules in order to the scene's tags: a rule whose
// tag matches a tag name or alias (case-insensitively) sets every field
// it carries, so later rules override earlier ones.
func ResolveFormat(rules []config.VideoRule, tags []*gql.TagPartsArrayTagsTag) Format {
	var f Format
	for _, r := range rules {
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
	}
	return f
}

func hasTag(tags []*gql.TagPartsArrayTagsTag, name string) bool {
	for _, t := range tags {
		if t != nil && util.StrSliceEquals(t.Name, t.Aliases, name) {
			return true
		}
	}
	return false
}
