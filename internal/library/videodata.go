package library

import (
	"stash-vr/internal/stash/gql"
)

type VideoData struct {
	SceneParts *gql.SceneParts
	// releaseDate is the date a stash-box gave when Stash has none; set
	// when the scene is cached.
	releaseDate string
}

func (vd VideoData) Title() string {
	if vd.SceneParts == nil {
		return ""
	}
	if vd.SceneParts.Title != nil && *vd.SceneParts.Title != "" {
		return *vd.SceneParts.Title
	}
	if len(vd.SceneParts.Files) > 0 && vd.SceneParts.Files[0] != nil && vd.SceneParts.Files[0].Basename != "" {
		return vd.SceneParts.Files[0].Basename
	}
	return vd.Id()
}

func (vd VideoData) Id() string {
	if vd.SceneParts == nil {
		return ""
	}
	return vd.SceneParts.Id
}

// stashDate is the scene's date as stored in Stash, or "".
func (vd VideoData) stashDate() string {
	if vd.SceneParts == nil || vd.SceneParts.Date == nil {
		return ""
	}
	return *vd.SceneParts.Date
}

// ReleaseDate returns the scene's date from Stash, else the date looked up on
// a stash-box, else "".
func (vd VideoData) ReleaseDate() string {
	if d := vd.stashDate(); d != "" {
		return d
	}
	return vd.releaseDate
}
