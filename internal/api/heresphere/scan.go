package heresphere

import (
	"context"
	"stash-vr/internal/library"
	"stash-vr/internal/util"
	"time"

	"github.com/rs/zerolog/log"
)

type scanDocDto struct {
	ScanData []scanDataDto `json:"scanData"`
}

type scanDataDto struct {
	id           string
	Link         string   `json:"link"`
	Title        string   `json:"title"`
	DateReleased *string  `json:"dateReleased,omitempty"`
	DateAdded    string   `json:"dateAdded,omitempty"`
	Duration     float64  `json:"duration,omitempty"`
	Rating       *float32 `json:"rating,omitempty"`
	Favorites    *int     `json:"favorites,omitempty"`
	Comments     *int     `json:"comments,omitempty"`
	IsFavorite   *bool    `json:"isFavorite,omitempty"`
	Tags         []tagDto `json:"tags,omitempty"`
}

// buildScan lists every scene that has a file. A scene the cache could not
// fetch or that Stash lists without a file is skipped, not fatal: one bad
// scene must not cost the whole library.
func buildScan(ctx context.Context, vds map[string]*library.VideoData, baseUrl string) (*scanDocDto, error) {
	scanDoc := scanDocDto{ScanData: make([]scanDataDto, 0, len(vds))}
	skipped := 0
	for id, vd := range vds {
		if firstFile(vd) == nil {
			log.Ctx(ctx).Debug().Str("id", id).Msg("/scan: scene without a file skipped")
			skipped++
			continue
		}
		scanData := videoDataToScanDataDto(ctx, vd, baseUrl)
		scanDoc.ScanData = append(scanDoc.ScanData, scanData)
	}
	log.Ctx(ctx).Debug().Int("scenes", len(scanDoc.ScanData)).Int("skipped", skipped).Msg("/scan")
	return &scanDoc, nil
}

// videoDataToScanDataDto builds the scan entry; vd must have a file.
func videoDataToScanDataDto(ctx context.Context, vd *library.VideoData, baseUrl string) scanDataDto {
	id := vd.Id()
	scanData := scanDataDto{
		id:        id,
		Link:      getVideoDataUrl(baseUrl, id),
		Title:     vd.Title(),
		DateAdded: vd.SceneParts.Created_at.Format(time.DateOnly),
		Duration:  vd.SceneParts.Files[0].Duration,
		Tags:      getTags(vd),
	}
	if vd.SceneParts.Date != nil {
		scanData.DateReleased = util.Ptr(util.NormalizeDate(*vd.SceneParts.Date))
	}
	if vd.SceneParts.Rating100 != nil {
		scanData.Rating = util.Ptr(float32(*vd.SceneParts.Rating100) / 20.0)
	}
	if vd.SceneParts.O_counter != nil {
		scanData.Favorites = vd.SceneParts.O_counter
	}
	if vd.SceneParts.Play_count != nil {
		scanData.Comments = util.Ptr(*vd.SceneParts.Play_count)
	}
	if isFavorite(vd) {
		scanData.IsFavorite = util.Ptr(true)
	}
	return scanData
}
