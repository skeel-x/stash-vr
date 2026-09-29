package filter

import (
	"context"
	"fmt"
	"stash-vr/internal/stash/gql"
	"strings"

	"github.com/rs/zerolog/log"
)

type Filter struct {
	FilterOpts  gql.FindFilterType
	SceneFilter gql.SceneFilterType
}

var perPage = -1

// SavedFilterToSceneFilter converts a Stash saved filter into the scene
// query it stands for. A nil object filter matches every scene and a nil
// find filter leaves sort and direction unset. Malformed criteria are
// reported as errors, never panics, so a bad saved filter only costs its
// own section.
func SavedFilterToSceneFilter(ctx context.Context, savedFilter gql.SavedFilterParts) (Filter, error) {
	if savedFilter.Mode != gql.FilterModeScenes {
		return Filter{}, fmt.Errorf("unsupported filter mode (%s)", savedFilter.Mode)
	}

	objects := map[string]any{}
	if savedFilter.Object_filter != nil {
		objects = *savedFilter.Object_filter
	}
	sceneFilter, err := parseObjectFilter(ctx, objects)
	if err != nil {
		return Filter{}, err
	}

	filter := Filter{
		FilterOpts:  gql.FindFilterType{Per_page: &perPage},
		SceneFilter: sceneFilter,
	}
	if ff := savedFilter.Find_filter; ff != nil {
		filter.FilterOpts.Direction = ff.Direction
		if ff.Sort != nil {
			sort := *ff.Sort
			if strings.HasPrefix(sort, "random_") {
				sort = "random"
			}
			filter.FilterOpts.Sort = &sort
		}
	}

	return filter, nil
}

func parseObjectFilter(ctx context.Context, objects map[string]any) (gql.SceneFilterType, error) {
	var sft gql.SceneFilterType
	for k, v := range objects {
		criterion, ok := v.(map[string]any)
		if !ok {
			return gql.SceneFilterType{}, fmt.Errorf("criterion %s: value is %T, expected object", k, v)
		}
		if err := setSceneFilterCriterion(ctx, k, criterion, &sft); err != nil {
			return gql.SceneFilterType{}, fmt.Errorf("criterion %s: %w", k, err)
		}
	}
	return sft, nil
}

// set parses a criterion with parse and stores the result in dst.
func set[T any](dst **T, c map[string]any, parse func(map[string]any) (*T, error)) error {
	v, err := parse(c)
	if err != nil {
		return err
	}
	*dst = v
	return nil
}

func setSceneFilterCriterion(ctx context.Context, criterionType string, criterionValue map[string]any, sceneFilter *gql.SceneFilterType) error {
	switch criterionType {
	case "audio_codec":
		return set(&sceneFilter.Audio_codec, criterionValue, parseStringCriterionInput)
	case "bitrate":
		return set(&sceneFilter.Bitrate, criterionValue, parseIntCriterionInput)
	case "captions":
		return set(&sceneFilter.Captions, criterionValue, parseCaptionCriterionInput)
	case "checksum":
		return set(&sceneFilter.Checksum, criterionValue, parseStringCriterionInput)
	case "code":
		return set(&sceneFilter.Code, criterionValue, parseStringCriterionInput)
	case "created_at":
		return set(&sceneFilter.Created_at, criterionValue, parseTimestampCriterionInput)
	case "date":
		return set(&sceneFilter.Date, criterionValue, parseDateCriterionInput)
	case "details":
		return set(&sceneFilter.Details, criterionValue, parseStringCriterionInput)
	case "director":
		return set(&sceneFilter.Director, criterionValue, parseStringCriterionInput)
	case "duplicated":
		return set(&sceneFilter.Duplicated, criterionValue, parseDuplicationCriterionInput)
	case "duration":
		return set(&sceneFilter.Duration, criterionValue, parseIntCriterionInput)
	case "file_count":
		return set(&sceneFilter.File_count, criterionValue, parseIntCriterionInput)
	case "framerate":
		return set(&sceneFilter.Framerate, criterionValue, parseIntCriterionInput)
	case "galleries":
		return set(&sceneFilter.Galleries, criterionValue, parseMultiCriterionInput)
	case "groups":
		return set(&sceneFilter.Groups, criterionValue, parseHierarchicalMultiCriterionInput)
	case "has_markers":
		return decodeSimple(criterionValue, &sceneFilter.Has_markers)
	case "id":
		return set(&sceneFilter.Id, criterionValue, parseIntCriterionInput)
	case "interactive":
		return decodeSimple(criterionValue, &sceneFilter.Interactive)
	case "interactive_speed":
		return set(&sceneFilter.Interactive_speed, criterionValue, parseIntCriterionInput)
	case "is_missing":
		return decodeSimple(criterionValue, &sceneFilter.Is_missing)
	case "last_played_at":
		return set(&sceneFilter.Last_played_at, criterionValue, parseTimestampCriterionInput)
	case "movies":
		return set(&sceneFilter.Movies, criterionValue, parseMultiCriterionInput)
	case "o_counter":
		return set(&sceneFilter.O_counter, criterionValue, parseIntCriterionInput)
	case "organized":
		return decodeSimple(criterionValue, &sceneFilter.Organized)
	case "orientation":
		return set(&sceneFilter.Orientation, criterionValue, parseOrientationCriterionInput)
	case "oshash":
		return set(&sceneFilter.Oshash, criterionValue, parseStringCriterionInput)
	case "path":
		return set(&sceneFilter.Path, criterionValue, parseStringCriterionInput)
	case "performer_age":
		return set(&sceneFilter.Performer_age, criterionValue, parseIntCriterionInput)
	case "performer_count":
		return set(&sceneFilter.Performer_count, criterionValue, parseIntCriterionInput)
	case "performer_favorite":
		return decodeSimple(criterionValue, &sceneFilter.Performer_favorite)
	case "performer_tags":
		return set(&sceneFilter.Performer_tags, criterionValue, parseHierarchicalMultiCriterionInput)
	case "performers":
		return set(&sceneFilter.Performers, criterionValue, parseMultiCriterionInput)
	case "phash":
		return set(&sceneFilter.Phash, criterionValue, parseStringCriterionInput)
	case "phash_distance":
		return set(&sceneFilter.Phash_distance, criterionValue, parsePhashDistanceCriterionInput)
	case "play_count":
		return set(&sceneFilter.Play_count, criterionValue, parseIntCriterionInput)
	case "play_duration":
		return set(&sceneFilter.Play_duration, criterionValue, parseIntCriterionInput)
	case "rating100":
		return set(&sceneFilter.Rating100, criterionValue, parseIntCriterionInput)
	case "resolution":
		return set(&sceneFilter.Resolution, criterionValue, parseResolutionCriterionInput)
	case "resume_time":
		return set(&sceneFilter.Resume_time, criterionValue, parseIntCriterionInput)
	case "stash_id_endpoint":
		return set(&sceneFilter.Stash_id_endpoint, criterionValue, parseStashIDCriterionInput)
	case "studios":
		return set(&sceneFilter.Studios, criterionValue, parseHierarchicalMultiCriterionInput)
	case "tag_count":
		return set(&sceneFilter.Tag_count, criterionValue, parseIntCriterionInput)
	case "tags":
		return set(&sceneFilter.Tags, criterionValue, parseHierarchicalMultiCriterionInput)
	case "title":
		return set(&sceneFilter.Title, criterionValue, parseStringCriterionInput)
	case "updated_at":
		return set(&sceneFilter.Updated_at, criterionValue, parseTimestampCriterionInput)
	case "url":
		return set(&sceneFilter.Url, criterionValue, parseStringCriterionInput)
	case "video_codec":
		return set(&sceneFilter.Video_codec, criterionValue, parseStringCriterionInput)
	default:
		log.Ctx(ctx).Debug().Str("type", criterionType).Interface("value", criterionValue).Msg("Ignoring unsupported criterion")
	}
	return nil
}
