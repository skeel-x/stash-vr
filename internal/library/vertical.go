package library

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"stash-vr/internal/config"
)

// VerticalOffsetField is the scene custom field the vrQualityTags plugin
// writes: the measured vertical misalignment between the eyes, in degrees.
const VerticalOffsetField = "vr_vertical_offset"

// The measured offsets that are corrected. Below about 0.25 degrees a
// vertical mismatch between the eyes is not noticed; from there on it is
// the common threshold for eye strain. Above about 1.5 degrees the
// measurement is not trusted to be a pure offset between the eyes (a bad
// match, a lens mismatch or a rotated rig), so it is left alone.
const (
	MinVerticalCorrection = 0.25
	MaxVerticalCorrection = 1.5
)

// Why a scene's vertical offset is or is not corrected.
const (
	VerticalApplied     = "applied"      // the generated profile pitches one eye by the offset
	VerticalNotMeasured = "not_measured" // the scene has no offset
	VerticalInvalid     = "invalid"      // the custom field is not a finite number
	VerticalOff         = "off"          // correct_vertical_stereo is off
	VerticalNotStereo   = "not_stereo"   // the rules do not make the scene SBS or TB
	VerticalRulePitch   = "rule_pitch"   // a video rule sets the pitch itself
	VerticalTooSmall    = "too_small"    // below MinVerticalCorrection
	VerticalTooLarge    = "too_large"    // above MaxVerticalCorrection
)

// VerticalCorrection is the outcome of CorrectVertical for one scene.
// Offset is the measured value in degrees when Measured.
type VerticalCorrection struct {
	Offset   float64
	Measured bool
	Applied  bool
	Reason   string
}

// VerticalOffset reads the measured vertical offset from the scene's
// custom fields. present reports that the field exists at all; ok that it
// holds a finite number (a JSON number or a numeric string).
func VerticalOffset(vd *VideoData) (offset float64, present, ok bool) {
	if vd == nil || vd.SceneParts == nil {
		return 0, false, false
	}
	raw, present := vd.SceneParts.Custom_fields[VerticalOffsetField]
	if !present {
		return 0, false, false
	}
	v, ok := toFloat(raw)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, true, false
	}
	return v, true, true
}

func toFloat(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

// CorrectVertical sets f's pitch to the scene's measured vertical offset
// and asks for a generated profile when on is true, the rules make the
// scene stereo (SBS or TB, not forced mono), no rule sets the pitch and
// the offset is within [MinVerticalCorrection, MaxVerticalCorrection] in
// either direction. HereSphere's alignment pitch rotates one eye, so the
// offset is used as measured, sign included. A stored profile still wins
// over the generated one (see ProfileSourceFor).
func CorrectVertical(f *Format, vd *VideoData, on bool) VerticalCorrection {
	offset, present, ok := VerticalOffset(vd)
	c := VerticalCorrection{Offset: offset, Measured: ok}
	abs := math.Abs(offset)
	switch {
	case !present:
		c.Reason = VerticalNotMeasured
	case !ok:
		c.Reason = VerticalInvalid
	case !on:
		c.Reason = VerticalOff
	case f.ForceMono || (f.Stereo != "sbs" && f.Stereo != "tb"):
		c.Reason = VerticalNotStereo
	case f.Pitch != nil:
		c.Reason = VerticalRulePitch
	case abs < MinVerticalCorrection:
		c.Reason = VerticalTooSmall
	case abs > MaxVerticalCorrection:
		c.Reason = VerticalTooLarge
	default:
		v := offset
		f.Pitch = &v
		f.Generated = true
		c.Applied = true
		c.Reason = VerticalApplied
	}
	return c
}

// SceneFormat resolves the video rules for vd and, when correct is true,
// applies the vertical stereo correction: the format a HereSphere profile
// is picked and generated from. Other players and the cover badges use
// ResolveFormat alone.
func SceneFormat(rules []config.VideoRule, vd *VideoData, correct bool) (Format, VerticalCorrection) {
	f := ResolveFormat(rules, vd.SceneParts.Tags)
	c := CorrectVertical(&f, vd, correct)
	return f, c
}
