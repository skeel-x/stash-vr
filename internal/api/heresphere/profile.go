package heresphere

import (
	"errors"
	"math"
	"net/http"
	"time"

	"stash-vr/internal/api/internal"
	"stash-vr/internal/hsp"
	"stash-vr/internal/library"
	"stash-vr/internal/util"
)

var errNoFiles = errors.New("scene has no files")

// GenerateProfile encodes the HereSphere profile stash-vr generates for a
// scene whose video rules set screen geometry, background or mask.
func GenerateProfile(r *http.Request, vd *library.VideoData, f library.Format) ([]byte, error) {
	return generateProfile(vd, internal.GetBaseUrl(r), f)
}

func generateProfile(vd *library.VideoData, baseUrl string, f library.Format) ([]byte, error) {
	if vd == nil || vd.SceneParts == nil || len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil {
		return nil, errNoFiles
	}
	return hsp.Encode(buildProfile(vd, baseUrl, f))
}

var (
	hspProjections = map[string]uint8{
		"perspective": hsp.ProjectionPerspective, "equirectangular": hsp.ProjectionEquirectangular,
		"fisheye": hsp.ProjectionFisheye, "equirectangular360": hsp.ProjectionEquirectangular360,
		"cubemap": hsp.ProjectionCubemap, "equiangularCubemap": hsp.ProjectionEquiangularCubemap,
	}
	hspStereo      = map[string]uint8{"mono": hsp.StereoMono, "sbs": hsp.StereoSideBySide, "tb": hsp.StereoTopBottom}
	hspBackgrounds = map[string]uint8{"global": hsp.BackgroundGlobal, "color": hsp.BackgroundColor, "passthrough": hsp.BackgroundPassthrough}
	hspMasks       = map[string]uint8{"none": hsp.MaskNone, "chroma": hsp.MaskChromaKey, "alpha": hsp.MaskAlphaPacked}
)

// buildProfile starts from hsp.Default and fills in what the HereSphere
// document says about the scene (id, title, dates, rating, tags) and what
// the rules say about the screen. vd must have a file.
func buildProfile(vd *library.VideoData, baseUrl string, f library.Format) *hsp.Profile {
	p := hsp.Default()
	sp := vd.SceneParts
	p.ID = getVideoDataUrl(baseUrl, vd.Id())
	p.Title = vd.Title()
	p.DateAdded = dateTicks(sp.Created_at.Format(time.DateOnly))
	if d := vd.ReleaseDate(); d != "" {
		p.DateReleased = dateTicks(util.NormalizeDate(d))
	}
	p.Duration = hsp.TimespanTicks(sp.Files[0].Duration)
	if sp.Rating100 != nil {
		p.AverageRating = float32(*sp.Rating100) / 20
	}
	if sp.Play_count != nil && *sp.Play_count > 0 {
		p.Comments = uint32(*sp.Play_count)
	}
	if sp.O_counter != nil && *sp.O_counter > 0 {
		p.Favorites = uint32(*sp.O_counter)
	}
	p.IsFavorite = isFavorite(vd)
	for _, t := range getTags(vd) {
		p.Tags = append(p.Tags, profileTag(t, p.Duration))
	}

	fk := &p.Format[0]
	if v, ok := hspProjections[f.Projection]; ok {
		fk.Projection = v
	}
	if v, ok := hspStereo[f.Stereo]; ok {
		fk.Stereo = v
	}
	setF32(&fk.Zoom.X, f.ZoomX)
	setF32(&fk.Zoom.Y, f.ZoomY)
	setF32(&fk.Pan.X, f.PanX)
	setF32(&fk.Pan.Y, f.PanY)

	lk := &p.Lens[0]
	if f.Lens != "" {
		lk.TrueLens, lk.ExportLens = f.Lens, f.Lens
	}
	if f.Fov != 0 {
		lk.TrueFOV, lk.ExportFOV = f.Fov, f.Fov
	}

	ak := &p.Alignment[0]
	setF32(&ak.Position.X, f.PositionX)
	setF32(&ak.Position.Y, f.PositionY)
	setF32(&ak.Position.Z, f.PositionZ)
	setF32(&ak.Rotation.Pitch, f.Pitch)
	setF32(&ak.Rotation.Yaw, f.Yaw)
	setF32(&ak.Rotation.Roll, f.Roll)

	ok := &p.Origin[0]
	setF32(&ok.Origin.X, f.OriginX)
	setF32(&ok.Origin.Y, f.OriginY)
	setF32(&ok.Origin.Z, f.OriginZ)

	ek := &p.Environment[0]
	switch {
	case f.Background != "":
		ek.Background = hspBackgrounds[f.Background]
	case f.Passthrough:
		ek.Background = hsp.BackgroundPassthrough
	}
	if f.BackgroundColor != "" {
		ek.BackgroundColor = hsp.ColorFromHex(f.BackgroundColor)
	}
	switch {
	case f.Mask != "":
		ek.Mask = hspMasks[f.Mask]
	case f.Passthrough:
		ek.Mask = hsp.MaskAlphaPacked
	}
	return p
}

// profileTag converts a document tag (times in milliseconds, no end
// meaning the whole scene) to a profile tag.
func profileTag(t tagDto, duration int64) hsp.Tag {
	out := hsp.Tag{Name: t.Name, Start: hsp.TimespanTicks(t.Start / 1000), End: duration}
	if t.End != nil {
		out.End = hsp.TimespanTicks(*t.End / 1000)
	}
	if t.Rating != nil {
		out.Rating = *t.Rating
	}
	if t.Track != nil && *t.Track >= 0 && *t.Track <= math.MaxUint32 {
		out.Track = uint32(*t.Track)
	}
	return out
}

func dateTicks(date string) int64 {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return 0
	}
	return hsp.DateTicks(t)
}

func setF32(dst *float32, v *float64) {
	if v != nil {
		*dst = float32(*v)
	}
}
