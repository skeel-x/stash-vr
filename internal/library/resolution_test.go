package library

import "testing"

func TestNearestResolution(t *testing.T) {
	for _, c := range []struct {
		in   int
		want int
		tier string
	}{
		{0, 240, "Low"},
		{540, 540, "Low"},
		{720, 720, "Medium"},
		{1080, 1080, "Medium"},
		{1920, 2160, "High"},
		{2880, 2160, "High"},
		{4096, 4320, "High"},
	} {
		got, tier := NearestResolution(c.in)
		if got != c.want || tier != c.tier {
			t.Errorf("NearestResolution(%d) = %d %q, want %d %q", c.in, got, tier, c.want, c.tier)
		}
	}
}

func TestResolutionLabel(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want string
	}{
		{8192, 4096, "8K"},
		{7680, 3840, "8K"},
		{7200, 3600, "7K"},
		{5760, 2880, "6K"},
		{5400, 2700, "5K"},
		{4096, 2048, "4K"},
		{3840, 2160, "4K"},
		{1920, 1080, "1080p"},
		{1280, 720, "720p"},
		// Width unknown: the height alone decides.
		{0, 1080, "1080p"},
		{0, 2160, "4K"},
		{0, 4320, "8K"},
		{0, 0, ""},
	} {
		if got := ResolutionLabel(c.w, c.h); got != c.want {
			t.Errorf("ResolutionLabel(%d, %d) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}
