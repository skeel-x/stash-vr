package coverbadge

import (
	"image"
	"math"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const (
	// heightShare and insetShare size the badges from the cover: 7% of its
	// height, inset by 2% of its width.
	heightShare = 0.07
	insetShare  = 0.02
	// minHeight keeps badges legible in the headset on small covers.
	minHeight = 18
	// maxHeightShare caps the height at a quarter of short covers.
	maxHeightShare = 0.25
	// minDrawable is the smallest height worth drawing; below it the
	// cover is left as it is.
	minDrawable = 10
	// Text size, horizontal padding, gap between badges and corner radius
	// as shares of the badge height.
	textShare    = 0.6
	paddingShare = 0.35
	gapShare     = 0.2
	radiusShare  = 0.25
)

// boldFont is the bundled Go Bold, parsed once. A parsed font is safe for
// concurrent use; faces are not, so every Draw makes its own.
var boldFont = sync.OnceValues(func() (*sfnt.Font, error) {
	return opentype.Parse(gobold.TTF)
})

// Draw returns a copy of img with badges drawn left to right in its bottom
// left corner, clear of the top left corner where HereSphere draws its
// own icons. bottomReserve is the height of the heatmap strip overlaid at
// the bottom, 0 for none; the badges sit the inset above it. The size is
// unchanged and img is not modified. Badges that do not fit the width
// are dropped; with no badges, or no room for one, img is returned as it
// is.
func Draw(img image.Image, badges []Badge, bottomReserve int) image.Image {
	if len(badges) == 0 {
		return img
	}
	b := img.Bounds()
	h := badgeHeight(b.Dy())
	if h < minDrawable {
		return img
	}
	inset := max(2, int(math.Round(float64(b.Dx())*insetShare)))
	y := b.Max.Y - max(0, bottomReserve) - inset - h
	if y < b.Min.Y {
		return img
	}
	fnt, err := boldFont()
	if err != nil {
		return img
	}
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{Size: float64(h) * textShare, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return img
	}
	defer func() { _ = face.Close() }()

	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)

	pad := int(math.Round(float64(h) * paddingShare))
	gap := max(2, int(math.Round(float64(h)*gapShare)))
	radius := float32(h) * radiusShare
	baseline := textBaseline(face, h)

	x := b.Min.X + inset
	for i := range badges {
		bg := &badges[i]
		w := font.MeasureString(face, bg.Label).Ceil() + 2*pad
		if x+w > b.Max.X-inset {
			break
		}
		fillRoundedRect(dst, image.Rect(x, y, x+w, y+h), radius, image.NewUniform(bg.Fill))
		d := font.Drawer{Dst: dst, Src: image.NewUniform(bg.Text), Face: face, Dot: fixed.P(x+pad, y+baseline)}
		d.DrawString(bg.Label)
		x += w + gap
	}
	return dst
}

// badgeHeight is 7% of the cover height, at least minHeight, at most a
// quarter of the cover.
func badgeHeight(coverHeight int) int {
	h := max(minHeight, int(math.Round(float64(coverHeight)*heightShare)))
	return min(h, int(float64(coverHeight)*maxHeightShare))
}

// textBaseline centres capital letters and digits vertically in a badge
// of height h, returning the baseline's offset from the badge top.
func textBaseline(face font.Face, h int) int {
	m := face.Metrics()
	capHeight := m.CapHeight
	if capHeight <= 0 {
		capHeight = m.Ascent * 7 / 10
	}
	return (fixed.I(h) + capHeight).Round() / 2
}

// fillRoundedRect paints r with rounded corners of radius rad,
// antialiased, over dst.
func fillRoundedRect(dst draw.Image, r image.Rectangle, rad float32, src image.Image) {
	w, h := float32(r.Dx()), float32(r.Dy())
	rad = min(rad, w/2, h/2)
	// k places cubic control points so each corner approximates a
	// quarter circle.
	k := rad * 0.5523
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	z.MoveTo(rad, 0)
	z.LineTo(w-rad, 0)
	z.CubeTo(w-rad+k, 0, w, rad-k, w, rad)
	z.LineTo(w, h-rad)
	z.CubeTo(w, h-rad+k, w-rad+k, h, w-rad, h)
	z.LineTo(rad, h)
	z.CubeTo(rad-k, h, 0, h-rad+k, 0, h-rad)
	z.LineTo(0, rad)
	z.CubeTo(0, rad-k, rad-k, 0, rad, 0)
	z.ClosePath()
	z.Draw(dst, r, src, image.Point{})
}
