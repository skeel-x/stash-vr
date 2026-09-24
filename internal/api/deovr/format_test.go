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

func TestSetFormat_ForceMonoTurnsStereoOff(t *testing.T) {
	for _, f := range []library.Format{
		{Projection: "equirectangular", Stereo: "sbs", ForceMono: true},
		{Projection: "fisheye", Stereo: "sbs", Fov: 190, ForceMono: true},
		{Projection: "fisheye", Stereo: "sbs", EyeSwap: true, ForceMono: true},
	} {
		var dto videoDataDto
		setFormat(&dto, f)
		if dto.StereoMode != "off" || !dto.Is3d {
			t.Errorf("%+v: stereo %q is3d %v", f, dto.StereoMode, dto.Is3d)
		}
	}
	var dto videoDataDto
	setFormat(&dto, library.Format{Projection: "equirectangular", Stereo: "sbs", EyeSwap: true})
	if dto.StereoMode != "sbs" || dto.ScreenType != "dome" {
		t.Fatalf("eye swap has no DeoVR field and must leave the mapping alone: %s/%s", dto.ScreenType, dto.StereoMode)
	}
}
