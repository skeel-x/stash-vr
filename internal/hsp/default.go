package hsp

// Default returns a profile with one keyframe per section, carrying the
// values HereSphere writes for a scene nobody adjusted: an equirectangular
// side-by-side screen at the origin, a linear 180 degree lens, identity
// stitching, neutral image and audio, and the global background. Callers
// fill in the scene fields and whatever geometry they know.
func Default() *Profile {
	neutral := Vec4{1, 1, 1, 1}
	black := Color{0, 0, 0, 1}
	chromaSettings := Color{1, 0.1, 0, 1}
	return &Profile{
		Version: Version,
		Tags:    []Tag{},
		Format: []FormatKey{{
			Projection: ProjectionEquirectangular,
			Stereo:     StereoSideBySide,
			Aspect:     2,
			Zoom:       Vec2{1, 1},
		}},
		Lens: []LensKey{{
			TrueLens:   "Linear",
			ExportLens: "Linear",
			TrueCal:    Vec4{X: 0.63661998510360717773}, // 2/pi, as HereSphere writes it
			ExportCal:  Vec4{X: 0.63661998510360717773},
			TrueFOV:    180,
			ExportFOV:  180,
		}},
		Stitch:      []StitchKey{{Scale: neutral}},
		Alignment:   []AlignmentKey{{}},
		Orientation: []OrientationKey{{}},
		Origin:      []OriginKey{{}},
		Motion:      []MotionKey{{Distance: 200}},
		AutoFocus:   []AutoFocusKey{{Min: 10, Max: 1000, Speed: 1}},
		Sync:        []SyncKey{{}},
		Transition:  []TransitionKey{{}},
		Image: []ImageKey{{
			Temperature: 6500,
			Saturation:  neutral,
			Contrast:    neutral,
			Gamma:       neutral,
			Gain:        neutral,
			Midtones:    1,
			Highlights:  1,
		}},
		Audio: []AudioKey{{Volume: 1}},
		Environment: []EnvironmentKey{{
			Background:      BackgroundGlobal,
			BackgroundColor: black,
			Mask:            MaskNone,
			Opacity:         2,
			ChromaKeys:      [6]Color{black, chromaSettings, black, chromaSettings, black, chromaSettings},
			Despill:         [2]Color{black, {0, 0.2, 0.2, 1}},
			Light:           Color{0.5, 0.5, 0.5, 1},
			AlphaCoords:     Color{0.25, -0.5, 0.4, 0.4},
		}},
	}
}
