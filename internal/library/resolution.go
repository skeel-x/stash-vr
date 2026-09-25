package library

import (
	"fmt"

	"stash-vr/internal/util"
)

var resolutions = []int{240, 360, 480, 540, 720, 1080, 1440, 2160, 4320}

// NearestResolution snaps a frame height to the closest common resolution
// and names its tier (Low, Medium or High). HereSphere's Resolution tags
// and the cover badges both use it.
func NearestResolution(n int) (int, string) {
	nearest := resolutions[0]
	minDiff := util.Abs(n - nearest)

	for _, r := range resolutions[1:] {
		if d := util.Abs(n - r); d < minDiff || (d == minDiff && r < nearest) {
			minDiff = d
			nearest = r
		}
	}

	var tier string
	switch {
	case nearest <= 540:
		tier = "Low"
	case nearest <= 1080:
		tier = "Medium"
	default:
		tier = "High"
	}

	return nearest, tier
}

// minKWidth is the frame width from which a file is named by its width in
// thousands of pixels (4K, 5K, 6K, 8K), the way VR releases are labelled.
const minKWidth = 3840

// ResolutionLabel names a file's resolution for display: "8K" or "5K" from
// the width for 4K-class and larger files, else the nearest common height
// such as "1080p". A zero width falls back to the height alone; "" means
// neither is known.
func ResolutionLabel(width, height int) string {
	if width >= minKWidth {
		return fmt.Sprintf("%dK", (width+500)/1000)
	}
	if height <= 0 {
		return ""
	}
	n, _ := NearestResolution(height)
	switch {
	case n >= 4320:
		return "8K"
	case n >= 2160:
		return "4K"
	}
	return fmt.Sprintf("%dp", n)
}
