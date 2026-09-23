package deovr

import (
	"testing"

	"stash-vr/internal/library"
)

func TestSetFormat_MapsResolvedFormatToDeoVR(t *testing.T) {
	cases := []struct {
		f    library.Format
		st   string
		sm   string
		is3d bool
	}{
		{library.Format{Projection: "equirectangular", Stereo: "sbs"}, "dome", "sbs", true},
		{library.Format{Projection: "equirectangular360", Stereo: "tb"}, "sphere", "tb", true},
		{library.Format{Projection: "fisheye", Stereo: "sbs", Lens: "MKX200", Fov: 200}, "mkx200", "sbs", true},
		{library.Format{Projection: "fisheye", Stereo: "sbs", Fov: 190}, "rf52", "cuv", true},
		{library.Format{Projection: "fisheye", Stereo: "sbs"}, "fisheye", "sbs", true},
		{library.Format{Projection: "perspective", Stereo: "mono"}, "flat", "off", true},
		{library.Format{Projection: "cubemap", Stereo: "sbs"}, "sphere", "sbs", true},
		{library.Format{Stereo: "sbs"}, "", "sbs", true},
		{library.Format{}, "", "", false},
	}
	for _, c := range cases {
		var dto videoDataDto
		setFormat(&dto, c.f)
		if dto.ScreenType != c.st || dto.StereoMode != c.sm || dto.Is3d != c.is3d {
			t.Errorf("%+v: got %s/%s/%v want %s/%s/%v", c.f, dto.ScreenType, dto.StereoMode, dto.Is3d, c.st, c.sm, c.is3d)
		}
	}
}
