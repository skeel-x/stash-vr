// Package coverbadge picks and draws the small labels stash-vr puts onto
// scene covers: the quality tier or resolution, the projection, a
// passthrough marker, the running time and the frame rate.
package coverbadge

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// Kind says which setting a badge belongs to.
type Kind string

const (
	KindQuality     Kind = "quality"
	KindFormat      Kind = "format"
	KindPassthrough Kind = "passthrough"
	KindDuration    Kind = "duration"
	KindFrameRate   Kind = "framerate"
)

// Badge is one label drawn onto a cover.
type Badge struct {
	Kind  Kind
	Label string
	Fill  color.RGBA
	Text  color.RGBA
}

// Badge colours: the quality tiers get metals with dark text, everything
// else white text on a dark or accent fill. Slate replaces the metal, and
// the grey of a resolution label, on scenes tagged Low Detail.
var (
	Gold    = color.RGBA{R: 0xd4, G: 0xaf, B: 0x37, A: 0xff}
	Silver  = color.RGBA{R: 0xc4, G: 0xc8, B: 0xcc, A: 0xff}
	Bronze  = color.RGBA{R: 0xcd, G: 0x7f, B: 0x32, A: 0xff}
	Slate   = color.RGBA{R: 0x5b, G: 0x65, B: 0x73, A: 0xff}
	Neutral = color.RGBA{R: 0x30, G: 0x30, B: 0x30, A: 0xff}
	Accent  = color.RGBA{R: 0x1e, G: 0x6e, B: 0xc8, A: 0xff}
	White   = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	Dark    = color.RGBA{R: 0x14, G: 0x14, B: 0x14, A: 0xff}
)

// tiers are the vrQualityTags tier tags, best first. The HQ parent tag is
// deliberately absent: it says a tier applies, not which.
var tiers = []struct {
	tag  string
	fill color.RGBA
}{
	{"8K", Gold},
	{"7K", Silver},
	{"6K HBR", Bronze},
}

// lowDetailTag is the tag vrQualityTags sets on scenes whose files do not
// carry the detail their resolution claims: upscales, or a bitrate too
// low for the tier.
const lowDetailTag = "Low Detail"

// TierTags returns the names of the tier tags, best first.
func TierTags() []string {
	out := make([]string, len(tiers))
	for i := range tiers {
		out[i] = tiers[i].tag
	}
	return out
}

// ForScene returns the badges a scene's cover gets under the switches in
// on, in drawing order: quality, format, passthrough, duration, frame
// rate. rules are the video rules the format and passthrough badges
// resolve through.
func ForScene(vd *library.VideoData, on config.CoverBadges, rules []config.VideoRule) []Badge {
	if vd == nil || vd.SceneParts == nil || !on.Any() {
		return nil
	}
	var out []Badge
	if on.Quality {
		if b, ok := qualityBadge(vd); ok {
			out = append(out, b)
		}
	}
	if on.Format || on.Passthrough {
		f := library.ResolveFormat(rules, vd.SceneParts.Tags)
		if on.Format {
			if label := formatLabel(&f); label != "" {
				out = append(out, Badge{Kind: KindFormat, Label: label, Fill: Neutral, Text: White})
			}
		}
		if on.Passthrough && isPassthrough(&f) {
			out = append(out, Badge{Kind: KindPassthrough, Label: "AR", Fill: Accent, Text: White})
		}
	}
	if on.Duration || on.FrameRate {
		out = append(out, fileBadges(vd, on)...)
	}
	return out
}

// fileBadges are the duration and frame rate badges from the primary
// file; each is left out when the file does not say.
func fileBadges(vd *library.VideoData, on config.CoverBadges) []Badge {
	files := vd.SceneParts.Files
	if len(files) == 0 || files[0] == nil {
		return nil
	}
	var out []Badge
	if on.Duration {
		if label := durationLabel(files[0].Duration); label != "" {
			out = append(out, Badge{Kind: KindDuration, Label: label, Fill: Neutral, Text: White})
		}
	}
	if on.FrameRate {
		if label := frameRateLabel(files[0].Frame_rate); label != "" {
			out = append(out, Badge{Kind: KindFrameRate, Label: label, Fill: Neutral, Text: White})
		}
	}
	return out
}

// durationLabel is the running time in whole minutes ("42 min", at least
// "1 min"), or hours and minutes from one hour ("1 h 05"); "" when the
// duration is unknown.
func durationLabel(seconds float64) string {
	if !(seconds > 0) || math.IsInf(seconds, 0) {
		return ""
	}
	minutes := max(1, int(math.Round(seconds/60)))
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d h %02d", minutes/60, minutes%60)
}

// frameRateLabel is the frame rate rounded to whole frames ("60 fps" for
// 59.94); "" when it is unknown.
func frameRateLabel(fps float64) string {
	if !(fps > 0) || math.IsInf(fps, 0) {
		return ""
	}
	return fmt.Sprintf("%d fps", int(math.Round(fps)))
}

// qualityBadge is the best tier tag in its metal, else the resolution of
// the primary file in grey; either turns slate when the scene is tagged
// Low Detail.
func qualityBadge(vd *library.VideoData) (Badge, bool) {
	b, ok := tierOrResolution(vd)
	if ok && hasTag(vd, lowDetailTag) {
		b.Fill, b.Text = Slate, White
	}
	return b, ok
}

func tierOrResolution(vd *library.VideoData) (Badge, bool) {
	for _, tier := range tiers {
		if hasTag(vd, tier.tag) {
			return Badge{Kind: KindQuality, Label: tier.tag, Fill: tier.fill, Text: Dark}, true
		}
	}
	files := vd.SceneParts.Files
	if len(files) == 0 || files[0] == nil {
		return Badge{}, false
	}
	label := library.ResolutionLabel(files[0].Width, files[0].Height)
	if label == "" {
		return Badge{}, false
	}
	return Badge{Kind: KindQuality, Label: label, Fill: Neutral, Text: White}, true
}

// hasTag reports a tag named name on the scene, ignoring case and
// surrounding spaces.
func hasTag(vd *library.VideoData, name string) bool {
	for _, t := range vd.SceneParts.Tags {
		if t != nil && strings.EqualFold(strings.TrimSpace(t.Name), name) {
			return true
		}
	}
	return false
}

// formatLabel names the projection; plain flat 2D and an unknown
// projection get no label.
func formatLabel(f *library.Format) string {
	switch f.Projection {
	case "equirectangular":
		return "180"
	case "equirectangular360", "cubemap", "equiangularCubemap":
		return "360"
	case "fisheye":
		if f.Fov > 0 {
			return fmt.Sprintf("FISHEYE %d", int(f.Fov+0.5))
		}
		return "FISHEYE"
	case "perspective":
		if f.Stereo == "sbs" || f.Stereo == "tb" {
			return "FLAT 3D"
		}
	}
	return ""
}

// isPassthrough reports an alpha matte or a chroma-key mask.
func isPassthrough(f *library.Format) bool {
	return f.Passthrough || f.Mask == "alpha" || f.Mask == "chroma"
}

// Key names a badge set for cache keys, colours included so a tier that
// turns slate is drawn afresh; "" for none.
func Key(badges []Badge) string {
	var b strings.Builder
	for i := range badges {
		if i > 0 {
			b.WriteByte('|')
		}
		bg := &badges[i]
		fmt.Fprintf(&b, "%s:%s#%02x%02x%02x", bg.Kind, bg.Label, bg.Fill.R, bg.Fill.G, bg.Fill.B)
	}
	return b.String()
}
