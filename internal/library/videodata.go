package library

import (
	"stash-vr/internal/stash/gql"
)

type VideoData struct {
	SceneParts *gql.SceneParts
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
