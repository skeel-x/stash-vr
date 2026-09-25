package coverbadge

import (
	"image"
	"image/color"
	"testing"
)

// solid returns a w x h image filled with c.
func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

var sky = color.RGBA{R: 40, G: 90, B: 160, A: 255}

func near(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

func rgba(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// unchanged fails the test when any pixel of want inside r differs in got.
func unchanged(t *testing.T, got, want image.Image, r image.Rectangle, what string) {
	t.Helper()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if rgba(got, x, y) != rgba(want, x, y) {
				t.Fatalf("%s: pixel %d,%d changed to %v", what, x, y, rgba(got, x, y))
			}
		}
	}
}

// hasNear reports whether any pixel in r is near c.
func hasNear(img image.Image, r image.Rectangle, c color.RGBA, tol int) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if near(rgba(img, x, y), c, tol) {
				return true
			}
		}
	}
	return false
}

var heat = color.RGBA{R: 200, G: 30, B: 30, A: 255}

// withStrip returns a w x h sky image whose bottom rows rows are heat, like
// a cover with the heatmap overlaid.
func withStrip(w, h, rows int) *image.RGBA {
	img := solid(w, h, sky)
	for y := h - rows; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, heat)
		}
	}
	return img
}

func TestDraw_KeepsSizeAndLeavesSourceAlone(t *testing.T) {
	src := solid(640, 320, sky)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}, 0)

	if out.Bounds() != src.Bounds() {
		t.Fatalf("size changed: %v -> %v", src.Bounds(), out.Bounds())
	}
	unchanged(t, src, solid(640, 320, sky), src.Bounds(), "Draw must not modify its input")
}

func TestDraw_PaintsTheBadgeBottomLeft(t *testing.T) {
	src := solid(1000, 500, sky)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}, 0)

	// Inset 2% of the width (20 px), height 7% of the cover (35 px), so the
	// badge spans rows 445 to 479.
	badge := image.Rect(20, 445, 120, 480)
	if !hasNear(out, badge, Gold, 4) {
		t.Fatal("expected gold fill in the bottom left badge region")
	}
	if !hasNear(out, badge, Dark, 30) {
		t.Fatal("expected dark text inside the gold badge")
	}
	unchanged(t, out, src, image.Rect(0, 480, 1000, 500), "the inset below the badge")
	unchanged(t, out, src, image.Rect(0, 0, 20, 500), "the inset left of the badge")
	unchanged(t, out, src, image.Rect(0, 0, 1000, 445), "everything above the badge")
}

func TestDraw_SitsAboveTheHeatmapStrip(t *testing.T) {
	src := withStrip(1000, 500, 40)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}, 40)

	// The strip covers rows 460 to 499; the badge keeps the 20 px inset
	// above it, rows 405 to 439.
	unchanged(t, out, src, image.Rect(0, 460, 1000, 500), "the heatmap strip")
	unchanged(t, out, src, image.Rect(0, 440, 1000, 460), "the inset between badge and strip")
	unchanged(t, out, src, image.Rect(0, 0, 1000, 405), "everything above the badge")
	if !hasNear(out, image.Rect(20, 405, 120, 440), Gold, 4) {
		t.Fatal("expected the gold badge right above the strip")
	}
}

func TestDraw_LeavesTheTopLeftAlone(t *testing.T) {
	// HereSphere draws its own icons in the top left corner.
	src := solid(400, 200, sky)
	badges := []Badge{
		{Kind: KindQuality, Label: "6K HBR", Fill: Bronze, Text: Dark},
		{Kind: KindFormat, Label: "FISHEYE 200", Fill: Neutral, Text: White},
		{Kind: KindPassthrough, Label: "AR", Fill: Accent, Text: White},
	}

	out := Draw(src, badges, 0)

	unchanged(t, out, src, image.Rect(0, 0, 400, 100), "the top half")
	if !hasNear(out, image.Rect(0, 100, 400, 200), Bronze, 4) {
		t.Fatal("expected the badges in the bottom half")
	}
}

func TestDraw_StacksBadgesLeftToRight(t *testing.T) {
	src := solid(1200, 600, sky)
	badges := []Badge{
		{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark},
		{Kind: KindPassthrough, Label: "AR", Fill: Accent, Text: White},
	}

	out := Draw(src, badges, 0)

	// Inset 24 px, height 42 px: the badges span rows 534 to 575. Scan
	// along their top edge, below the rounded corners but above the text.
	y := 534 + 4
	firstGold, firstAccent, gapAfterGold := -1, -1, false
	for x := 0; x < 1200; x++ {
		c := rgba(out, x, y)
		switch {
		case near(c, Gold, 4) && firstGold < 0:
			firstGold = x
		case near(c, Accent, 4) && firstAccent < 0:
			firstAccent = x
		case c == sky && firstGold >= 0 && firstAccent < 0:
			gapAfterGold = true
		}
	}
	if firstGold < 0 || firstAccent < 0 || firstGold > firstAccent {
		t.Fatalf("expected gold then accent along the row, got gold at %d, accent at %d", firstGold, firstAccent)
	}
	if !gapAfterGold {
		t.Fatal("expected a gap between the badges")
	}
}

func TestDraw_MinimumHeightOnSmallCovers(t *testing.T) {
	// 7% of 200 is 14 px; the badge still gets 18, rows 174 to 191 above
	// the 8 px inset.
	src := solid(400, 200, sky)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}, 0)

	top, bottom := -1, -1
	for y := 0; y < 200; y++ {
		if near(rgba(out, 8+9, y), Gold, 4) {
			if top < 0 {
				top = y
			}
			bottom = y
		}
	}
	if top != 174 || bottom != 191 {
		t.Fatalf("expected the badge on rows 174 to 191, got %d to %d", top, bottom)
	}
}

func TestDraw_NoBadgesAndTinyCovers(t *testing.T) {
	src := solid(64, 32, sky)
	if out := Draw(src, nil, 0); out != image.Image(src) {
		t.Fatal("no badges must return the input unchanged")
	}
	tiny := solid(16, 12, sky)
	out := Draw(tiny, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}, 0)
	if out.Bounds() != tiny.Bounds() {
		t.Fatal("a tiny cover keeps its size")
	}
}

func TestDraw_NoRoomAboveATallStrip(t *testing.T) {
	src := withStrip(400, 200, 190)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}, 190)

	if out != image.Image(src) {
		t.Fatal("a strip leaving no room for a badge must leave the cover as it is")
	}
}

func TestDraw_NegativeReserveCountsAsNone(t *testing.T) {
	src := solid(400, 200, sky)
	badges := []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}}

	a, b := Draw(src, badges, -30), Draw(src, badges, 0)

	unchanged(t, a, b, src.Bounds(), "a negative reserve")
}

func TestDraw_SkipsBadgesThatDoNotFit(t *testing.T) {
	src := solid(120, 400, sky)
	badges := []Badge{
		{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark},
		{Kind: KindFormat, Label: "FISHEYE 200", Fill: Neutral, Text: White},
	}

	out := Draw(src, badges, 0)

	unchanged(t, out, src, image.Rect(119, 0, 120, 400), "a badge must not run past the right edge")
}
