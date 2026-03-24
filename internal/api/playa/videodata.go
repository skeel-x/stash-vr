package playa

import (
	"fmt"
	"slices"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type videoChoiceKey struct {
	isStream     bool
	isDownload   bool
	projection   string
	stereo       string
	qualityOrder int
}

type videoLinkCandidate struct {
	link       VideoLinkView
	resolution int
	isDirect   bool
}

func buildVideoListView(vd *library.VideoData, baseURL string) VideoListView {
	var releaseDate *int64
	if vd != nil && vd.SceneParts != nil {
		releaseDate = releaseDateUnix(vd.SceneParts.Date)
	}
	view := VideoListView{
		ID:           vd.Id(),
		Title:        vd.Title(),
		Status:       publishedStatusID,
		PreviewImage: previewImage(vd, baseURL),
		HasScripts:   hasScripts(vd),
		ReleaseDate:  releaseDate,
		Details:      buildVideoListDetails(vd),
	}
	view.Subtitle = sceneSubtitle(vd)
	return view
}

func buildVideoView(vd *library.VideoData, savedFilters []library.SavedFilterSceneSet, baseURL string) VideoView {
	videoID := vd.Id()
	var releaseDate *int64
	var views *int
	if vd != nil && vd.SceneParts != nil {
		releaseDate = releaseDateUnix(vd.SceneParts.Date)
		views = vd.SceneParts.Play_count
	}
	view := VideoView{
		ID:           videoID,
		Title:        vd.Title(),
		Status:       publishedStatusID,
		PreviewImage: previewImage(vd, baseURL),
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
		if slices.Contains(filter.SceneIDs, videoID) {
			view.Categories = append(view.Categories, CategoryRef{ID: savedFilterCategoryPrefix + filter.ID, Title: filter.Name})
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
		previewURL := keyedURL(vd.SceneParts.Paths.Preview)
		if previewURL == nil {
			return []VideoLinkView{unavailableVideoLink(false, projection, stereo, qualityName(resolution), qualityOrder(resolution), "offline")}
		}
		return []VideoLinkView{{
			IsStream:     true,
			IsDownload:   false,
			URL:          previewURL,
			Projection:   projection,
			Stereo:       stereo,
			QualityName:  qualityName(resolution),
			QualityOrder: qualityOrder(resolution),
		}}
	}

	candidates := buildPlayableLinkCandidates(vd, projection, stereo)
	if len(candidates) == 0 {
		return []VideoLinkView{unavailableVideoLink(true, projection, stereo, qualityNameWithDirectSuffix(resolution), qualityOrder(resolution), "offline")}
	}

	slices.SortFunc(candidates, func(a, b videoLinkCandidate) int {
		switch {
		case a.link.QualityOrder > b.link.QualityOrder:
			return -1
		case a.link.QualityOrder < b.link.QualityOrder:
			return 1
		case a.isDirect && !b.isDirect:
			return -1
		case !a.isDirect && b.isDirect:
			return 1
		case a.resolution > b.resolution:
			return -1
		case a.resolution < b.resolution:
			return 1
		default:
			return 0
		}
	})
	deduped := dedupeLinks(candidates)
	log.Debug().Str("video_id", vd.Id()).Interface("links", deduped).Msg("Generated video links")
	return deduped
}

func buildPlayableLinkCandidates(vd *library.VideoData, projection string, stereo string) []videoLinkCandidate {
	sp := vd.SceneParts
	nativeResolution := fileResolution(vd)
	candidates := make([]videoLinkCandidate, 0, 4)

	if sp.Paths != nil && sp.Paths.Stream != nil && *sp.Paths.Stream != "" && len(sp.Files) > 0 && sp.Files[0] != nil {
		for _, source := range stash.GetDirectStream(sp).Sources {
			if source.Url == "" {
				continue
			}
			keyed := stash.ApiKeyed(source.Url)
			directOrder := preferredDirectQualityOrder(source.Resolution)
			candidates = append(candidates, videoLinkCandidate{
				link: VideoLinkView{
					IsStream:     true,
					IsDownload:   true,
					URL:          &keyed,
					Projection:   projection,
					Stereo:       stereo,
					QualityName:  qualityNameWithDirectSuffix(source.Resolution),
					QualityOrder: directOrder,
				},
				resolution: source.Resolution,
				isDirect:   true,
			})
		}
	}

	if len(sp.Files) == 0 || sp.Files[0] == nil {
		return candidates
	}

	for _, source := range stash.GetTranscodingStream(sp).Sources {
		if source.Url == "" {
			continue
		}
		if nativeResolution > 0 && source.Resolution > nativeResolution {
			continue
		}
		keyed := stash.ApiKeyed(source.Url)
		candidates = append(candidates, videoLinkCandidate{
			link: VideoLinkView{
				IsStream:     true,
				IsDownload:   true,
				URL:          &keyed,
				Projection:   projection,
				Stereo:       stereo,
				QualityName:  qualityName(source.Resolution),
				QualityOrder: qualityOrder(source.Resolution),
			},
			resolution: source.Resolution,
		})
	}

	return candidates
}

func unavailableVideoLink(isDownload bool, projection string, stereo string, quality string, order int, reason string) VideoLinkView {
	message := reason
	return VideoLinkView{
		IsStream:          true,
		IsDownload:        isDownload,
		Projection:        projection,
		Stereo:            stereo,
		QualityName:       quality,
		QualityOrder:      order,
		UnavailableReason: &message,
	}
}

func dedupeLinks(candidates []videoLinkCandidate) []VideoLinkView {
	seen := map[videoChoiceKey]struct{}{}
	out := make([]VideoLinkView, 0, len(candidates))
	for _, candidate := range candidates {
		link := candidate.link
		key := videoChoiceKey{
			isStream:     link.IsStream,
			isDownload:   link.IsDownload,
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

func previewImage(vd *library.VideoData, baseURL string) *string {
	if vd == nil || vd.SceneParts == nil || vd.SceneParts.Paths == nil || baseURL == "" {
		return nil
	}
	if vd.SceneParts.Paths.Screenshot != nil && *vd.SceneParts.Paths.Screenshot != "" {
		posterURL := baseURL + "/api/playa/v2/poster/" + vd.Id()
		return &posterURL
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

func qualityNameWithDirectSuffix(resolution int) string {
	return qualityName(resolution) + " (DS)"
}

func preferredDirectQualityOrder(resolution int) int {
	switch {
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

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
