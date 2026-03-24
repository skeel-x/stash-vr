package playa

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"stash-vr/internal/library"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type videoSortItem struct {
	vd         *library.VideoData
	lowerTitle string
	playCount  int
	release    int64
	hasRelease bool
	numericID  int
	hasID      bool
}

type videoQuery struct {
	PageIndex          int
	PageSize           int
	Order              string
	Direction          string
	Title              string
	ActorID            string
	StudioID           string
	IncludedCategories []categoryKey
	ExcludedCategories []categoryKey
	IncludedStatuses   []string
	ExcludedStatuses   []string
}

func (h httpHandler) buildVideoPage(ctx context.Context, query videoQuery, baseURL string) (Page[VideoListView], error) {
	allIDs, err := h.libraryService.GetAllSceneIDs(ctx)
	if err != nil {
		return Page[VideoListView]{}, err
	}
	candidateSet := sliceToSet(allIDs)
	savedFilters, err := h.libraryService.GetSavedFilterSceneSets(ctx)
	if err != nil {
		return Page[VideoListView]{}, err
	}
	filterLookup := savedFilterLookup(savedFilters)

	for _, category := range query.IncludedCategories {
		ids, err := resolveCategorySceneSet(ctx, h.libraryService, category, filterLookup)
		if err != nil {
			return Page[VideoListView]{}, err
		}
		candidateSet = intersectSets(candidateSet, ids)
	}

	excluded := map[string]struct{}{}
	for _, category := range query.ExcludedCategories {
		ids, err := resolveCategorySceneSet(ctx, h.libraryService, category, filterLookup)
		if err != nil {
			return Page[VideoListView]{}, err
		}
		for id := range ids {
			excluded[id] = struct{}{}
		}
	}
	candidateSet = subtractSet(candidateSet, excluded)

	if len(query.IncludedStatuses) > 0 {
		if !slices.Contains(query.IncludedStatuses, publishedStatusID) {
			candidateSet = map[string]struct{}{}
		}
	}
	if slices.Contains(query.ExcludedStatuses, publishedStatusID) {
		candidateSet = map[string]struct{}{}
	}

	startFetch := time.Now()
	allScenesMap, err := h.libraryService.GetScenes(ctx)
	if err != nil {
		return Page[VideoListView]{}, err
	}
	log.Ctx(ctx).Debug().Dur("duration", time.Since(startFetch)).Msg("Successfully fetched scenes from cache")

	filtered := make([]*library.VideoData, 0, len(candidateSet))
	for id := range candidateSet {
		vd := allScenesMap[id]
		if vd == nil {
			continue
		}
		if query.Title != "" && !strings.Contains(strings.ToLower(vd.Title()), strings.ToLower(query.Title)) {
			continue
		}
		if query.ActorID != "" && !sceneHasActor(vd, query.ActorID) {
			continue
		}
		if query.StudioID != "" && !sceneHasStudio(vd, query.StudioID) {
			continue
		}
		filtered = append(filtered, vd)
	}

	sortVideoData(filtered, query.Order, query.Direction)
	items := make([]VideoListView, 0, len(filtered))
	for _, vd := range filtered {
		items = append(items, buildVideoListView(vd, baseURL))
	}
	return paginate(items, query.PageIndex, query.PageSize), nil
}

func sortVideoData(items []*library.VideoData, order string, direction string) {
	decorated := make([]videoSortItem, 0, len(items))
	for _, vd := range items {
		item := videoSortItem{vd: vd, lowerTitle: strings.ToLower(vd.Title())}
		if vd != nil && vd.SceneParts != nil {
			if vd.SceneParts.Play_count != nil {
				item.playCount = *vd.SceneParts.Play_count
			}
			if released := releaseDateUnix(vd.SceneParts.Date); released != nil {
				item.release = *released
				item.hasRelease = true
			}
		}
		item.numericID, item.hasID = numericID(vd.Id())
		decorated = append(decorated, item)
	}

	slices.SortStableFunc(decorated, func(left, right videoSortItem) int {
		switch order {
		case "release_date":
			if !left.hasRelease && !right.hasRelease {
				c := strings.Compare(left.lowerTitle, right.lowerTitle)
				if c == 0 {
					c = compareEntityIDs(left.vd.Id(), left.numericID, left.hasID, right.vd.Id(), right.numericID, right.hasID)
				}
				return c
			}
			if !left.hasRelease {
				return 1
			}
			if !right.hasRelease {
				return -1
			}
			c := cmp.Compare(left.release, right.release)
			if direction == "desc" {
				c = -c
			}
			if c == 0 {
				c = strings.Compare(left.lowerTitle, right.lowerTitle)
			}
			if c == 0 {
				c = compareEntityIDs(left.vd.Id(), left.numericID, left.hasID, right.vd.Id(), right.numericID, right.hasID)
			}
			return c
		case "popularity":
			c := cmp.Compare(left.playCount, right.playCount)
			if direction == "desc" {
				c = -c
			}
			if c == 0 {
				c = strings.Compare(left.lowerTitle, right.lowerTitle)
			}
			if c == 0 {
				c = compareEntityIDs(left.vd.Id(), left.numericID, left.hasID, right.vd.Id(), right.numericID, right.hasID)
			}
			return c
		default:
			c := strings.Compare(left.lowerTitle, right.lowerTitle)
			if direction == "desc" {
				c = -c
			}
			if c == 0 {
				c = compareEntityIDs(left.vd.Id(), left.numericID, left.hasID, right.vd.Id(), right.numericID, right.hasID)
			}
			return c
		}
	})

	for i, item := range decorated {
		items[i] = item.vd
	}
}

func sceneHasActor(vd *library.VideoData, actorID string) bool {
	if vd == nil || vd.SceneParts == nil {
		return false
	}
	for _, performer := range vd.SceneParts.Performers {
		if performer != nil && performer.Id == actorID {
			return true
		}
	}
	return false
}

func sceneHasStudio(vd *library.VideoData, studioID string) bool {
	if vd == nil || vd.SceneParts == nil {
		return false
	}
	return vd.SceneParts.Studio != nil && vd.SceneParts.Studio.Id == studioID
}

func paginate[T any](items []T, pageIndex int, pageSize int) Page[T] {
	itemTotal := len(items)
	pageTotal := 1
	if itemTotal > 0 {
		pageTotal = (itemTotal + pageSize - 1) / pageSize
	}
	start := pageIndex * pageSize
	if start > itemTotal {
		start = itemTotal
	}
	end := start + pageSize
	if end > itemTotal {
		end = itemTotal
	}
	content := make([]T, 0, end-start)
	if start < end {
		content = append(content, items[start:end]...)
	}
	return Page[T]{PageIndex: pageIndex, PageSize: pageSize, PageTotal: pageTotal, ItemTotal: itemTotal, Content: content}
}

func validateVideoOrder(order string) error {
	switch order {
	case "title", "release_date", "popularity":
		return nil
	default:
		return fmt.Errorf("unsupported order: %s", order)
	}
}
