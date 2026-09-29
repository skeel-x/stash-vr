package playa

import (
	"cmp"
	"context"
	"fmt"
	"math/rand"
	"slices"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/library"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type videoSortItem struct {
	vd         *library.VideoData
	lowerTitle string
	playCount  int
	createdAt  int64
	hasCreated bool
	numericID  int
	hasID      bool
}

type videoQuery struct {
	PageIndex          int
	PageSize           int
	Order              string
	Direction          string
	Randomize          bool
	Title              string
	ActorID            string
	StudioID           string
	IncludedCategories []categoryKey
	ExcludedCategories []categoryKey
	IncludedStatuses   []string
	ExcludedStatuses   []string
}

// buildVideoPage answers /videos: the scenes of the index (the scene cache,
// which GetScenes fills) narrowed by the query, ordered, and cut to the
// requested page. List views are built for that page only; the rest of the
// library is filtered and sorted as bare scene data.
func (h httpHandler) buildVideoPage(ctx context.Context, query videoQuery, baseURL string) (Page[VideoListView], error) {
	startFetch := time.Now()
	allScenesMap, err := h.libraryService.GetScenes(ctx)
	if err != nil {
		return Page[VideoListView]{}, err
	}
	log.Ctx(ctx).Debug().Dur("duration", time.Since(startFetch)).Msg("Successfully fetched scenes from cache")

	savedFilters, err := h.libraryService.GetSavedFilterSceneSetsFor(ctx, "playa")
	if err != nil {
		return Page[VideoListView]{}, err
	}
	filterLookup := savedFilterLookup(savedFilters)

	candidateSet := make(map[string]struct{}, len(allScenesMap))
	for id := range allScenesMap {
		candidateSet[id] = struct{}{}
	}
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

	lowerTitle := strings.ToLower(query.Title)
	filtered := make([]*library.VideoData, 0, len(candidateSet))
	for id := range candidateSet {
		vd := allScenesMap[id]
		if vd == nil {
			continue
		}
		if lowerTitle != "" && !strings.Contains(strings.ToLower(vd.Title()), lowerTitle) {
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

	if query.Randomize {
		shuffleVideoData(filtered, h.libraryService.IndexBuiltAt().UnixNano())
	} else {
		sortVideoData(filtered, query.Order, query.Direction)
	}

	start, end, pageTotal := pageBounds(len(filtered), query.PageIndex, query.PageSize)
	posterQuery := coverbadge.CurrentURLQuery()
	items := make([]VideoListView, 0, end-start)
	for _, vd := range filtered[start:end] {
		items = append(items, buildVideoListView(vd, baseURL, posterQuery))
	}
	return Page[VideoListView]{PageIndex: query.PageIndex, PageSize: query.PageSize, PageTotal: pageTotal, ItemTotal: len(filtered), Content: items}, nil
}

// shuffleVideoData puts items in a random order that depends on seed and
// the set of items only: the scene cache is a map, so items is first put
// in id order. Playa asks for a random listing one page at a time, and the
// same seed across those requests keeps a scene from being skipped or
// listed twice.
func shuffleVideoData(items []*library.VideoData, seed int64) {
	if len(items) < 2 {
		return
	}
	slices.SortFunc(items, func(left, right *library.VideoData) int {
		l, lok := numericID(left.Id())
		r, rok := numericID(right.Id())
		return compareEntityIDs(left.Id(), l, lok, right.Id(), r, rok)
	})
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(items), func(i int, j int) {
		items[i], items[j] = items[j], items[i]
	})
}

func sortVideoData(items []*library.VideoData, order string, direction string) {
	decorated := make([]videoSortItem, 0, len(items))
	for _, vd := range items {
		item := videoSortItem{vd: vd, lowerTitle: strings.ToLower(vd.Title())}
		if vd != nil && vd.SceneParts != nil {
			if vd.SceneParts.Play_count != nil {
				item.playCount = *vd.SceneParts.Play_count
			}
			if !vd.SceneParts.Created_at.IsZero() {
				item.createdAt = vd.SceneParts.Created_at.Unix()
				item.hasCreated = true
			}
		}
		item.numericID, item.hasID = numericID(vd.Id())
		decorated = append(decorated, item)
	}

	slices.SortStableFunc(decorated, func(left, right videoSortItem) int {
		switch order {
		case "release_date":
			if !left.hasCreated && !right.hasCreated {
				c := strings.Compare(left.lowerTitle, right.lowerTitle)
				if c == 0 {
					c = compareEntityIDs(left.vd.Id(), left.numericID, left.hasID, right.vd.Id(), right.numericID, right.hasID)
				}
				return c
			}
			if !left.hasCreated {
				return 1
			}
			if !right.hasCreated {
				return -1
			}
			c := cmp.Compare(left.createdAt, right.createdAt)
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

// pageBounds is the half-open slice [start, end) of page pageIndex in
// itemTotal items, and how many pages there are (at least one).
func pageBounds(itemTotal int, pageIndex int, pageSize int) (start, end, pageTotal int) {
	pageTotal = 1
	if itemTotal > 0 {
		pageTotal = (itemTotal + pageSize - 1) / pageSize
	}
	// A page past the last one is empty. Checked by division so a page
	// index near MaxInt cannot overflow the multiplication into a negative
	// start and a slice panic.
	if pageIndex > itemTotal/pageSize {
		return itemTotal, itemTotal, pageTotal
	}
	start = min(pageIndex*pageSize, itemTotal)
	end = min(start+pageSize, itemTotal)
	return start, end, pageTotal
}

func paginate[T any](items []T, pageIndex int, pageSize int) Page[T] {
	start, end, pageTotal := pageBounds(len(items), pageIndex, pageSize)
	content := make([]T, 0, end-start)
	content = append(content, items[start:end]...)
	return Page[T]{PageIndex: pageIndex, PageSize: pageSize, PageTotal: pageTotal, ItemTotal: len(items), Content: content}
}

func validateVideoOrder(order string) error {
	switch order {
	case "title", "release_date", "popularity":
		return nil
	default:
		return fmt.Errorf("unsupported order: %s", order)
	}
}
