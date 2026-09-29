package deovr

import (
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/library"
	"stash-vr/internal/util"
)

type indexDto struct {
	Authorized string     `json:"authorized"`
	Scenes     []sceneDto `json:"scenes"`
}

type sceneDto struct {
	Name string           `json:"name"`
	List []previewDataDto `json:"list"`
}

type previewDataDto struct {
	Id           string  `json:"id"`
	ThumbnailUrl *string `json:"thumbnailUrl"`
	Title        string  `json:"title"`
	VideoLength  int     `json:"videoLength"`
	VideoUrl     string  `json:"video_url"`
}

// buildIndex lists each section's scenes. A scene id the cache could not
// resolve, or a scene without a file, is left out of its section rather
// than failing the whole index.
func buildIndex(sections []library.Section, vds map[string]*library.VideoData, baseUrl string) (indexDto, error) {
	index := indexDto{Authorized: "1", Scenes: make([]sceneDto, len(sections))}

	for i, section := range sections {
		s := sceneDto{
			Name: section.Name,
			List: make([]previewDataDto, 0, len(section.Ids)),
		}

		for _, sectionSceneId := range section.Ids {
			vd := vds[sectionSceneId]
			if vd == nil || vd.SceneParts == nil || len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil {
				continue
			}
			preview := previewDataDto{
				Id:          vd.SceneParts.Id,
				Title:       vd.Title(),
				VideoLength: int(vd.SceneParts.Files[0].Duration),
				VideoUrl:    getVideoDataUrl(baseUrl, vd.Id()),
			}
			if vd.SceneParts.Paths != nil && vd.SceneParts.Paths.Screenshot != nil && *vd.SceneParts.Paths.Screenshot != "" {
				preview.ThumbnailUrl = util.Ptr(heatmap.GetCoverUrl(baseUrl, vd.Id()))
			}
			s.List = append(s.List, preview)
		}
		index.Scenes[i] = s
	}

	return index, nil
}
