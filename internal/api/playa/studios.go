package playa

import (
	"cmp"
	"fmt"
	"slices"
	"stash-vr/internal/library"
	"strings"
)

type studioSortItem struct {
	studio    library.Studio
	lowerName string
	numericID int
	hasID     bool
}

func buildStudioPage(studios []library.Studio, pageIndex int, pageSize int, order string, direction string) Page[StudioListView] {
	sortStudios(studios, order, direction)
	items := make([]StudioListView, 0, len(studios))
	for _, studio := range studios {
		items = append(items, StudioListView{ID: studioPrefix + studio.ID, Title: studio.Name, Preview: keyedURL(studio.ImagePath)})
	}
	return paginate(items, pageIndex, pageSize)
}

func buildStudioView(studio *library.Studio) StudioView {
	return StudioView{
		ID:          studioPrefix + studio.ID,
		Title:       studio.Name,
		Preview:     keyedURL(studio.ImagePath),
		Description: studio.Details,
	}
}

func sortStudios(items []library.Studio, order string, direction string) {
	decorated := make([]studioSortItem, 0, len(items))
	for _, studio := range items {
		numericID, hasID := numericID(studio.ID)
		decorated = append(decorated, studioSortItem{studio: studio, lowerName: strings.ToLower(studio.Name), numericID: numericID, hasID: hasID})
	}

	slices.SortStableFunc(decorated, func(left, right studioSortItem) int {
		if order == "popularity" {
			c := cmp.Compare(left.studio.SceneCount, right.studio.SceneCount)
			if direction == "desc" {
				c = -c
			}
			if c == 0 {
				c = strings.Compare(left.lowerName, right.lowerName)
			}
			if c == 0 {
				c = compareEntityIDs(left.studio.ID, left.numericID, left.hasID, right.studio.ID, right.numericID, right.hasID)
			}
			return c
		}
		c := strings.Compare(left.lowerName, right.lowerName)
		if direction == "desc" {
			c = -c
		}
		if c == 0 {
			c = compareEntityIDs(left.studio.ID, left.numericID, left.hasID, right.studio.ID, right.numericID, right.hasID)
		}
		return c
	})

	for i, studio := range decorated {
		items[i] = studio.studio
	}
}

func validateStudioOrder(order string) error {
	switch order {
	case "title", "popularity":
		return nil
	default:
		return fmt.Errorf("unsupported order: %s", order)
	}
}
