package hsp

import (
	"fmt"
	"math"
	"strconv"
)

// ColorFromHex converts an sRGB "#rrggbb" colour to the linear colour
// Unreal stores, opaque. Anything else gives opaque black.
func ColorFromHex(s string) Color {
	if len(s) != 7 || s[0] != '#' {
		return Color{A: 1}
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return Color{A: 1}
	}
	return Color{R: toLinear(uint8(v >> 16)), G: toLinear(uint8(v >> 8)), B: toLinear(uint8(v)), A: 1}
}

// Hex converts c to an sRGB "#rrggbb" colour, ignoring alpha.
func (c Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", toSRGB(c.R), toSRGB(c.G), toSRGB(c.B))
}

func toLinear(v uint8) float32 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return float32(c / 12.92)
	}
	return float32(math.Pow((c+0.055)/1.055, 2.4))
}

func toSRGB(v float32) uint8 {
	c := math.Min(math.Max(float64(v), 0), 1)
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return uint8(math.Round(c * 255))
}
