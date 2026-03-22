package playa

import (
	"fmt"
	"regexp"
	"slices"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

var resolutionFromLabel = regexp.MustCompile(`\((\d+)p\)`)

type videoLinkKey struct {
	isStream     bool
	isDownload   bool
	url          string
	projection   string
	stereo       string
	qualityOrder int
}

func buildVideoListView(vd *library.VideoData) VideoListView {
	var releaseDate *int64
	if vd != nil && vd.SceneParts != nil {
		releaseDate = releaseDateUnix(vd.SceneParts.Date)
	}
	view := VideoListView{
		ID:           vd.Id(),
		Title:        vd.Title(),
		Status:       publishedStatusID,
		PreviewImage: previewImage(vd),
		HasScripts:   hasScripts(vd),
		ReleaseDate:  releaseDate,
		Details:      buildVideoListDetails(vd),
	}
	view.Subtitle = sceneSubtitle(vd)
	return view
}

func buildVideoView(vd *library.VideoData, savedFilters []library.SavedFilterSceneSet) VideoView {
	var releaseDate *int64
	var views *int
	if vd != nil && vd.SceneParts != nil {
		releaseDate = releaseDateUnix(vd.SceneParts.Date)
		views = vd.SceneParts.Play_count
	}
	view := VideoView{
		ID:           vd.Id(),
		Title:        vd.Title(),
		Status:       publishedStatusID,
		PreviewImage: previewImage(vd),
		ReleaseDate:  releaseDate,
		Views:        views,
		Details:      buildVideoDetails(vd),
	}
	view.Subtitle = sceneSubtitle(vd)
	if vd != nil && vd.SceneParts != nil && vd.SceneParts.Studio != nil && vd.SceneParts.Studio.Id != "" {
		view.Studio = &StudioRef{ID: studioPrefix + vd.SceneParts.Studio.Id, Title: vd.SceneParts.Studio.Name}
	}
	if vd != nil && vd.SceneParts != nil {
		for _, performer := range vd.SceneParts.Performers {
			if performer == nil || performer.Id == "" {
				continue
			}
			view.Actors = append(view.Actors, ActorRef{ID: actorPrefix + performer.Id, Title: performer.Name})
		}
	}
	for _, filter := range savedFilters {
		for _, id := range filter.SceneIDs {
			if id == vd.Id() {
				view.Categories = append(view.Categories, CategoryRef{ID: savedFilterCategoryPrefix + filter.ID, Title: filter.Name})
				break
			}
		}
	}
	for _, tag := range realSceneTags(vd) {
		view.Categories = append(view.Categories, CategoryRef{ID: tagCategoryPrefix + tag.Id, Title: tag.Name})
	}
	slices.SortFunc(view.Actors, func(a, b ActorRef) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	slices.SortFunc(view.Categories, func(a, b CategoryRef) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	return view
}

func buildVideoListDetails(vd *library.VideoData) []VideoListDetail {
	details := []VideoListDetail{{Type: "full", DurationSeconds: durationSeconds(vd), HasScripts: hasScripts(vd)}}
	if vd != nil && vd.SceneParts != nil && vd.SceneParts.Paths != nil && vd.SceneParts.Paths.Preview != nil {
		details = append(details, VideoListDetail{Type: "trailer", HasScripts: false})
	}
	return details
}

func buildVideoDetails(vd *library.VideoData) []VideoDetail {
	full := VideoDetail{
		Type:            "full",
		DurationSeconds: durationSeconds(vd),
		TimelineMarkers: buildTimelineMarkers(vd),
		Links:           buildVideoLinks(vd, false),
		ScriptInfo:      buildScriptInfo(vd),
	}
	details := make([]VideoDetail, 0, 2)
	if vd != nil && vd.SceneParts != nil && vd.SceneParts.Paths != nil && vd.SceneParts.Paths.Preview != nil {
		details = append(details, VideoDetail{Type: "trailer", Links: buildVideoLinks(vd, true)})
	}
	details = append(details, full)
	return details
}

func buildTimelineMarkers(vd *library.VideoData) []TimelineMarker {
	if vd == nil || vd.SceneParts == nil {
		return nil
	}
	markers := make([]TimelineMarker, 0, len(vd.SceneParts.Scene_markers))
	for _, marker := range vd.SceneParts.Scene_markers {
		if marker == nil {
			continue
		}
		title := marker.Title
		if title == "" && marker.Primary_tag != nil {
			title = marker.Primary_tag.Name
		} else if title != "" && marker.Primary_tag != nil {
			title = fmt.Sprintf("%s: %s", marker.Primary_tag.Name, title)
		}
		var titlePtr *string
		if title != "" {
			titleCopy := title
			titlePtr = &titleCopy
		}
		markers = append(markers, TimelineMarker{Time: int64(marker.Seconds * 1000), Title: titlePtr})
	}
	slices.SortFunc(markers, func(a, b TimelineMarker) int {
		switch {
		case a.Time < b.Time:
			return -1
		case a.Time > b.Time:
			return 1
		default:
			return 0
		}
	})
	return markers
}

func buildVideoLinks(vd *library.VideoData, trailer bool) []VideoLinkView {
	if vd == nil || vd.SceneParts == nil {
		return nil
	}
	projection, stereo := projectionAndStereo(vd)
	resolution := fileResolution(vd)
	if trailer {
		if vd.SceneParts.Paths == nil || vd.SceneParts.Paths.Preview == nil {
			return nil
		}
		keyed := stash.ApiKeyed(*vd.SceneParts.Paths.Preview)
		return []VideoLinkView{{
			IsStream:     true,
			IsDownload:   false,
			URL:          &keyed,
			Projection:   projection,
			Stereo:       stereo,
			QualityName:  qualityName(resolution),
			QualityOrder: qualityOrder(resolution),
		}}
	}

	links := make([]VideoLinkView, 0, 1)
	if vd.SceneParts.Paths != nil && vd.SceneParts.Paths.Stream != nil {
		keyed := stash.ApiKeyed(*vd.SceneParts.Paths.Stream)
		links = append(links, VideoLinkView{
			IsStream:     true,
			IsDownload:   true,
			URL:          &keyed,
			Projection:   projection,
			Stereo:       stereo,
			QualityName:  qualityName(resolution),
			QualityOrder: qualityOrder(resolution),
		})
	}

	for _, stream := range vd.SceneParts.SceneStreams {
		if stream == nil || stream.Url == "" {
			continue
		}
		if stream.Label != nil && *stream.Label == "Direct stream" {
			continue
		}
		res := resolution
		if stream.Label != nil {
			res = resolutionFromStreamLabel(*stream.Label, resolution)
		}
		keyed := stash.ApiKeyed(stream.Url)
		links = append(links, VideoLinkView{
			IsStream:     true,
			IsDownload:   true,
			URL:          &keyed,
			Projection:   projection,
			Stereo:       stereo,
			QualityName:  qualityName(res),
			QualityOrder: qualityOrder(res) - 1,
		})
	}
	slices.SortFunc(links, func(a, b VideoLinkView) int {
		switch {
		case a.QualityOrder > b.QualityOrder:
			return -1
		case a.QualityOrder < b.QualityOrder:
			return 1
		default:
			return 0
		}
	})
	return dedupeLinks(links)
}

func dedupeLinks(links []VideoLinkView) []VideoLinkView {
	seen := map[videoLinkKey]struct{}{}
	out := make([]VideoLinkView, 0, len(links))
	for _, link := range links {
		key := videoLinkKey{
			isStream:     link.IsStream,
			isDownload:   link.IsDownload,
			url:          derefString(link.URL),
			projection:   link.Projection,
			stereo:       link.Stereo,
			qualityOrder: link.QualityOrder,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, link)
	}
	return out
}

func projectionAndStereo(vd *library.VideoData) (string, string) {
	projection := "FLT"
	stereo := "MN"
	hasGenericVR := false

	for _, tag := range realSceneTags(vd) {
		switch {
		case equalsLegendTag(tag, internal.TagVR_DOME):
			projection = "180"
			if stereo == "MN" {
				stereo = "LR"
			}
		case equalsLegendTag(tag, internal.TagVR_SPHERE):
			projection = "360"
			if stereo == "MN" {
				stereo = "LR"
			}
		case equalsLegendTag(tag, internal.TagVR_FISHEYE), equalsLegendTag(tag, internal.TagVR_MKX200), equalsLegendTag(tag, internal.TagVR_RF52):
			projection = "FSH"
			if stereo == "MN" {
				stereo = "LR"
			}
		case equalsLegendTag(tag, internal.TagVR_TB):
			stereo = "TB"
		case equalsLegendTag(tag, internal.TagVR_SBS):
			if stereo == "MN" {
				stereo = "LR"
			}
		case equalsLegendTag(tag, "VR"), equalsLegendTag(tag, "Virtual Reality"):
			hasGenericVR = true
		}
	}

	if projection == "FLT" && stereo != "MN" {
		projection = "180"
	} else if projection == "FLT" && stereo == "MN" && hasGenericVR {
		projection = "180"
		stereo = "LR"
	}

	resolution := fileResolution(vd)
	log.Debug().
		Str("video_id", vd.Id()).
		Int("resolution", resolution).
		Bool("hasGenericVR", hasGenericVR).
		Str("assigned_projection", projection).
		Str("assigned_stereo", stereo).
		Msg("Playa VR evaluated tags")

	return projection, stereo
}

func equalsLegendTag(tag library.Tag, expected string) bool {
	return util.StrSliceEquals(tag.Name, tag.Aliases, expected)
}

func buildScriptInfo(vd *library.VideoData) *VideoScriptInfo {
	if vd == nil || vd.SceneParts == nil {
		return nil
	}
	if vd.SceneParts.Paths == nil || vd.SceneParts.Paths.Funscript == nil || *vd.SceneParts.Paths.Funscript == "" {
		return nil
	}
	return &VideoScriptInfo{ID: vd.Id(), GenerationSource: 0}
}

func releaseDateUnix(date *string) *int64 {
	if date == nil || *date == "" {
		return nil
	}
	parsed, err := time.Parse(time.DateOnly, *date)
	if err != nil {
		return nil
	}
	value := parsed.Unix()
	return &value
}

func durationSeconds(vd *library.VideoData) *int {
	if vd == nil || vd.SceneParts == nil || len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil {
		return nil
	}
	duration := int(vd.SceneParts.Files[0].Duration)
	return &duration
}

func fileResolution(vd *library.VideoData) int {
	if vd == nil || vd.SceneParts == nil || len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil {
		return 0
	}
	return vd.SceneParts.Files[0].Height
}

func sceneSubtitle(vd *library.VideoData) *string {
	if vd == nil || vd.SceneParts == nil {
		return nil
	}
	if vd.SceneParts.Studio != nil && vd.SceneParts.Studio.Name != "" {
		subtitle := vd.SceneParts.Studio.Name
		return &subtitle
	}
	if len(vd.SceneParts.Performers) > 0 && vd.SceneParts.Performers[0] != nil {
		subtitle := vd.SceneParts.Performers[0].Name
		return &subtitle
	}
	return nil
}

func keyedURL(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	keyed := stash.ApiKeyed(*value)
	return &keyed
}

func previewImage(vd *library.VideoData) *string {
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Paths == nil {
		return nil
	}
	if preview := keyedURL(vd.SceneParts.Paths.Screenshot); preview != nil {
		return preview
	}
	return keyedURL(vd.SceneParts.Paths.Preview)
}

func realSceneTags(vd *library.VideoData) []library.Tag {
	if vd == nil || vd.SceneParts == nil {
		return nil
	}
	tags := make([]library.Tag, 0, len(vd.SceneParts.Tags))
	seen := map[string]struct{}{}
	for _, tag := range vd.SceneParts.Tags {
		if tag == nil || tag.Id == "" || strings.HasPrefix(tag.Sort_name, "svr.ancestor") {
			continue
		}
		if _, ok := seen[tag.Id]; ok {
			continue
		}
		seen[tag.Id] = struct{}{}
		tags = append(tags, library.Tag{Id: tag.Id, Name: tag.Name, SortName: tag.Sort_name})
	}
	return tags
}

func hasScripts(vd *library.VideoData) bool {
	if vd == nil || vd.SceneParts == nil {
		return false
	}
	return vd.SceneParts.Paths != nil && vd.SceneParts.Paths.Funscript != nil && *vd.SceneParts.Paths.Funscript != ""
}

func qualityName(resolution int) string {
	if resolution <= 0 {
		return "auto"
	}
	return fmt.Sprintf("%dp", resolution)
}

func qualityOrder(resolution int) int {
	switch {
	case resolution >= 4320:
		return 49
	case resolution >= 3240:
		return 48
	case resolution >= 2880:
		return 47
	case resolution >= 2160:
		return 45
	case resolution >= 1440:
		return 35
	case resolution >= 1080:
		return 25
	case resolution >= 720:
		return 15
	case resolution >= 480:
		return 10
	default:
		return 5
	}
}

func resolutionFromStreamLabel(label string, fallback int) int {
	match := resolutionFromLabel.FindStringSubmatch(label)
	if len(match) != 2 {
		return fallback
	}
	var resolution int
	_, err := fmt.Sscanf(match[1], "%d", &resolution)
	if err != nil {
		return fallback
	}
	return resolution
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
