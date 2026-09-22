package library

import (
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"slices"
	"stash-vr/internal/config"
	"stash-vr/internal/stash"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
	"time"
)

func (libraryService *Service) UpdateRating(ctx context.Context, id string, rating5 *float32) error {
	var newRating100 *int
	if rating5 != nil {
		converted := int(*rating5 * 20)
		newRating100 = &converted
	}

	_, err := gql.SceneUpdateRating100(ctx, libraryService.Client(), id, newRating100)
	if err != nil {
		return fmt.Errorf("SceneUpdateRating100: %w", err)
	}
	return nil
}

func (libraryService *Service) UpdateFavorite(ctx context.Context, id string, isFavoriteRequested bool) error {
	favoriteTagName := config.Application().FavoriteTag

	if favoriteTagName == "" {
		log.Ctx(ctx).Info().Msg("Sync favorite requested but FAVORITE_TAG is empty, ignoring request")
		return nil
	}

	favoriteTagId, err := stash.FindOrCreateTag(ctx, libraryService.Client(), favoriteTagName)
	if err != nil {
		return err
	}

	response, err := gql.FindSceneTags(ctx, libraryService.Client(), id)
	if err != nil {
		return fmt.Errorf("FindSceneTags: %w", err)
	}

	newTagIds := make([]string, 0, len(response.FindScene.Tags)+1)

	var hasFavoriteTag bool
	for _, t := range response.FindScene.Tags {
		if t.Id == favoriteTagId {
			hasFavoriteTag = true
			if !isFavoriteRequested {
				continue
			}
		}
		newTagIds = append(newTagIds, t.Id)
	}
	if !hasFavoriteTag && isFavoriteRequested {
		newTagIds = append(newTagIds, favoriteTagId)
	}

	if _, err := gql.SceneUpdateTags(ctx, libraryService.Client(), id, newTagIds); err != nil {
		return fmt.Errorf("SceneUpdateTags: %w", err)
	}

	return nil
}

func (libraryService *Service) UpdateTags(ctx context.Context, id string, tags []string) error {
	tagIds := make([]string, len(tags))
	for i, tag := range tags {
		tagId, err := stash.FindOrCreateTag(ctx, libraryService.Client(), tag)
		if err != nil {
			return err
		}
		tagIds[i] = tagId
	}
	if _, err := gql.SceneUpdateTags(ctx, libraryService.Client(), id, tagIds); err != nil {
		return fmt.Errorf("SceneUpdateTags: %w", err)
	}
	return nil
}

type MarkerDto struct {
	PrimaryTagName string
	StartSecond    float64
	EndSecond      *float64
	Title          string
	MarkerId       string //hack: use the rating field for transport of marker id
}

func (libraryService *Service) UpdateMarkers(ctx context.Context, id string, incomingMarkers []MarkerDto) error {
	vd, err := libraryService.GetScene(ctx, id, false)
	if err != nil {
		return err
	}

	markersToDestroy := make([]string, 0)
	for _, existingMarker := range vd.SceneParts.Scene_markers {
		if !slices.ContainsFunc(incomingMarkers, func(m MarkerDto) bool {
			return m.MarkerId == existingMarker.Id
		}) {
			markersToDestroy = append(markersToDestroy, existingMarker.Id)
		}
	}

	markersToUpdate := make([]MarkerDto, 0)
	markersToCreate := make([]MarkerDto, 0)

	for _, incoming := range incomingMarkers {
		if incoming.MarkerId != "" && incoming.MarkerId != "0" && slices.ContainsFunc(vd.SceneParts.Scene_markers, func(existingMarker *gql.ScenePartsScene_markersSceneMarker) bool {
			return incoming.MarkerId == existingMarker.Id
		}) {
			markersToUpdate = append(markersToUpdate, incoming)
		} else {
			markersToCreate = append(markersToCreate, incoming)
		}
	}

	for _, m := range markersToUpdate {
		tagId, err := stash.FindOrCreateTag(ctx, libraryService.Client(), m.PrimaryTagName)
		if err != nil {
			return fmt.Errorf("failed to find or create primary tag for marker: %w", err)
		}
		_, err = gql.SceneMarkerUpdate(ctx, libraryService.Client(), m.MarkerId, tagId, m.StartSecond, m.EndSecond, m.Title)
		if err != nil {
			return fmt.Errorf("SceneMarkerCreate: %w", err)
		}
	}
	for _, m := range markersToCreate {
		tagId, err := stash.FindOrCreateTag(ctx, libraryService.Client(), m.PrimaryTagName)
		if err != nil {
			return fmt.Errorf("failed to find or create primary tag for marker: %w", err)
		}
		_, err = gql.SceneMarkerCreate(ctx, libraryService.Client(), id, tagId, m.StartSecond, m.EndSecond, m.Title)
		if err != nil {
			return fmt.Errorf("SceneMarkerCreate: %w", err)
		}
	}

	_, err = gql.SceneMarkersDestroy(ctx, libraryService.Client(), markersToDestroy)
	if err != nil {
		return fmt.Errorf("SceneMarkersDestroy: %w", err)
	}

	return nil
}

func (libraryService *Service) ClearAndCreateMarkers(ctx context.Context, id string, markers []MarkerDto) error {
	resp, err := gql.FindSceneMarkers(ctx, libraryService.Client(), id)
	if err != nil {
		return fmt.Errorf("FindSceneMarkers: %w", err)
	}
	currentMarkers := make([]MarkerDto, len(resp.FindSceneMarkers.Scene_markers))
	for i, m := range resp.FindSceneMarkers.Scene_markers {
		currentMarkers[i] = MarkerDto{
			PrimaryTagName: m.Primary_tag.Name,
			StartSecond:    m.Seconds * 1000,
			Title:          m.Title,
		}
		if m.End_seconds != nil {
			currentMarkers[i].EndSecond = util.Ptr(*m.End_seconds * 1000)
		}
	}
	if util.UnorderedEqual(currentMarkers, markers) {
		return nil
	}
	markersToDestroy := make([]string, len(resp.FindSceneMarkers.Scene_markers))
	for i, sm := range resp.FindSceneMarkers.Scene_markers {
		markersToDestroy[i] = sm.Id
	}
	_, err = gql.SceneMarkersDestroy(ctx, libraryService.Client(), markersToDestroy)
	if err != nil {
		return fmt.Errorf("SceneMarkersDestroy: %w", err)
	}

	for _, m := range markers {
		tagId, err := stash.FindOrCreateTag(ctx, libraryService.Client(), m.PrimaryTagName)
		if err != nil {
			return fmt.Errorf("failed to find or create primary tag for marker: %w", err)
		}
		_, err = gql.SceneMarkerCreate(ctx, libraryService.Client(), id, tagId, m.StartSecond, m.EndSecond, m.Title)
		if err != nil {
			return fmt.Errorf("SceneMarkerCreate: %w", err)
		}
	}
	return nil
}

func (libraryService *Service) Delete(ctx context.Context, id string) error {
	if _, err := gql.SceneDestroy(ctx, libraryService.Client(), id); err != nil {
		return fmt.Errorf("SceneDestroy: %w", err)
	}
	return nil
}

func (libraryService *Service) IncrementO(ctx context.Context, id string) error {
	_, err := gql.SceneIncrementO(ctx, libraryService.Client(), id)
	if err != nil {
		return fmt.Errorf("SceneIncrementO: %w", err)
	}
	return nil
}

func (libraryService *Service) DecrementO(ctx context.Context, id string) error {
	_, err := gql.SceneDecrementO(ctx, libraryService.Client(), id)
	if err != nil {
		return fmt.Errorf("SceneDecrementO: %w", err)
	}
	return nil
}

func (libraryService *Service) IncrementPlayCount(ctx context.Context, id string) error {
	_, err := gql.SceneIncrementPlayCount(ctx, libraryService.Client(), id)
	if err != nil {
		return fmt.Errorf("SceneIncrementPlayCount: %w", err)
	}
	return nil
}

func (libraryService *Service) DecrementPlayCount(ctx context.Context, id string) error {
	_, err := gql.SceneDecrementPlayCount(ctx, libraryService.Client(), id)
	if err != nil {
		return fmt.Errorf("SceneDecrementPlayCount: %w", err)
	}
	return nil
}

func (libraryService *Service) SetOrganized(ctx context.Context, id string, newState bool) error {
	_, err := gql.SceneUpdateOrganized(ctx, libraryService.Client(), id, &newState)
	if err != nil {
		return fmt.Errorf("SceneUpdateOrganized: %w", err)
	}
	return nil
}

func (libraryService *Service) AddPlayDuration(ctx context.Context, id string, duration time.Duration) error {
	seconds := duration.Seconds()
	_, err := gql.SceneAddPlayDurationSeconds(ctx, libraryService.Client(), id, &seconds)
	if err != nil {
		return fmt.Errorf("SceneAddPlayDurationSeconds: %w", err)
	}
	return nil
}

// SaveResumeTime stores the position Stash should resume the scene from.
// Zero clears it.
func (libraryService *Service) SaveResumeTime(ctx context.Context, id string, seconds float64) error {
	_, err := gql.SceneSaveResumeTime(ctx, libraryService.Client(), id, &seconds)
	if err != nil {
		return fmt.Errorf("SceneSaveResumeTime: %w", err)
	}
	return nil
}

// SaveActivity reports one playback stop to Stash: the seconds played since
// the last report and the position to resume from (0 clears it). Either
// value may be nil to leave it unchanged.
func (libraryService *Service) SaveActivity(ctx context.Context, id string, playedSeconds *float64, resume *float64) error {
	_, err := gql.SceneSaveActivity(ctx, libraryService.Client(), id, playedSeconds, resume)
	if err != nil {
		return fmt.Errorf("SceneSaveActivity: %w", err)
	}
	return nil
}
