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

func TestDraw_KeepsSizeAndLeavesSourceAlone(t *testing.T) {
	src := solid(640, 320, sky)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}})

	if out.Bounds() != src.Bounds() {
		t.Fatalf("size changed: %v -> %v", src.Bounds(), out.Bounds())
	}
	if rgba(src, 30, 20) != sky {
		t.Fatal("Draw must not modify its input")
	}
}

func TestDraw_PaintsTheBadgeTopLeft(t *testing.T) {
	src := solid(1000, 500, sky)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}})

	// Inset 2% of the width (20 px), height 7% of the cover (35 px).
	h := 35
	var gold, text bool
	for y := 20; y < 20+h; y++ {
		for x := 20; x < 120; x++ {
			c := rgba(out, x, y)
			gold = gold || near(c, Gold, 4)
			text = text || near(c, Dark, 30)
		}
	}
	if !gold {
		t.Fatal("expected gold fill in the badge region")
	}
	if !text {
		t.Fatal("expected dark text inside the gold badge")
	}
	if c := rgba(out, 5, 5); c != sky {
		t.Fatalf("the inset margin must stay untouched, got %v", c)
	}
	if c := rgba(out, 20+h/2, 20+h+6); c != sky {
		t.Fatalf("below the badge must stay untouched, got %v", c)
	}
}

func TestDraw_LeavesTheBottomStripAlone(t *testing.T) {
	src := solid(400, 200, sky)
	badges := []Badge{
		{Kind: KindQuality, Label: "6K HBR", Fill: Bronze, Text: Dark},
		{Kind: KindFormat, Label: "FISHEYE 200", Fill: Neutral, Text: White},
		{Kind: KindPassthrough, Label: "AR", Fill: Accent, Text: White},
	}

	out := Draw(src, badges)

	// Everything below the top half, where the heatmap strip sits, is
	// untouched.
	for y := 100; y < 200; y++ {
		for x := 0; x < 400; x++ {
			if c := rgba(out, x, y); c != sky {
				t.Fatalf("pixel %d,%d changed to %v", x, y, c)
			}
		}
	}
}

func TestDraw_StacksBadgesLeftToRight(t *testing.T) {
	src := solid(1200, 600, sky)
	badges := []Badge{
		{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark},
		{Kind: KindPassthrough, Label: "AR", Fill: Accent, Text: White},
	}

	out := Draw(src, badges)

	// Scan along the top edge of the badges, below the rounded corners
	// but above the text.
	y := 24 + 4
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
	// 7% of 200 is 14 px; the badge still gets 18.
	src := solid(400, 200, sky)

	out := Draw(src, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}})

	inset := 8
	bottom := -1
	for y := 0; y < 100; y++ {
		if near(rgba(out, inset+9, y), Gold, 4) {
			bottom = y
		}
	}
	if bottom < inset+17 {
		t.Fatalf("expected the badge to reach at least 18 px high, bottom row %d", bottom)
	}
}

func TestDraw_NoBadgesAndTinyCovers(t *testing.T) {
	src := solid(64, 32, sky)
	if out := Draw(src, nil); out != image.Image(src) {
		t.Fatal("no badges must return the input unchanged")
	}
	tiny := solid(16, 12, sky)
	out := Draw(tiny, []Badge{{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark}})
	if out.Bounds() != tiny.Bounds() {
		t.Fatal("a tiny cover keeps its size")
	}
}

func TestDraw_SkipsBadgesThatDoNotFit(t *testing.T) {
	src := solid(120, 400, sky)
	badges := []Badge{
		{Kind: KindQuality, Label: "8K", Fill: Gold, Text: Dark},
		{Kind: KindFormat, Label: "FISHEYE 200", Fill: Neutral, Text: White},
	}

	out := Draw(src, badges)

	for y := 0; y < 400; y++ {
		if c := rgba(out, 119, y); c != sky {
			t.Fatalf("a badge must not run past the right edge, pixel at row %d is %v", y, c)
		}
	}
}
