package playa

import (
	"cmp"
	"fmt"
	"slices"
	"stash-vr/internal/library"
	"strconv"
	"strings"
)

type actorSortItem struct {
	performer *library.Performer
	lowerName string
	numericID int
	hasID     bool
}

func buildActorPage(performers []library.Performer, pageIndex int, pageSize int, order string, direction string, title string) Page[ActorListView] {
	needle := strings.ToLower(title)
	filtered := make([]actorSortItem, 0, len(performers))
	for i := range performers {
		p := &performers[i]
		if needle != "" && !matchesPerformerTitle(*p, needle) {
			continue
		}
		numericID, hasID := numericID(p.ID)
		filtered = append(filtered, actorSortItem{performer: p, lowerName: strings.ToLower(p.Name), numericID: numericID, hasID: hasID})
	}
	sortActors(filtered, order, direction)
	items := make([]ActorListView, 0, len(filtered))
	for _, item := range filtered {
		performer := item.performer
		items = append(items, ActorListView{ID: actorPrefix + performer.ID, Title: performer.Name, Preview: keyedURL(performer.ImagePath)})
	}
	return paginate(items, pageIndex, pageSize)
}

func buildActorView(performer *library.Performer) ActorView {
	view := ActorView{
		ID:      actorPrefix + performer.ID,
		Title:   performer.Name,
		Preview: keyedURL(performer.ImagePath),
		Aliases: slices.Clone(performer.Aliases),
	}
	for _, studio := range performer.Studios {
		view.Studios = append(view.Studios, StudioRef{ID: studioPrefix + studio.ID, Title: studio.Name})
	}
	if performer.Birthdate != nil && *performer.Birthdate != "" {
		view.Properties = append(view.Properties, ActorProperty{Name: "Birthdate", Value: *performer.Birthdate})
	}
	if performer.Country != nil && *performer.Country != "" {
		view.Properties = append(view.Properties, ActorProperty{Name: "Country", Value: *performer.Country})
	}
	return view
}

func sortActors(items []actorSortItem, order string, direction string) {
	slices.SortStableFunc(items, func(a, b actorSortItem) int {
		if order == "popularity" {
			c := cmp.Compare(a.performer.SceneCount, b.performer.SceneCount)
			if direction == "desc" {
				c = -c
			}
			if c == 0 {
				c = strings.Compare(a.lowerName, b.lowerName)
			}
			if c == 0 {
				c = compareEntityIDs(a.performer.ID, a.numericID, a.hasID, b.performer.ID, b.numericID, b.hasID)
			}
			return c
		}

		c := strings.Compare(a.lowerName, b.lowerName)
		if direction == "desc" {
			c = -c
		}
		if c == 0 {
			c = compareEntityIDs(a.performer.ID, a.numericID, a.hasID, b.performer.ID, b.numericID, b.hasID)
		}
		return c
	})
}

func validateActorOrder(order string) error {
	switch order {
	case "title", "popularity":
		return nil
	default:
		return fmt.Errorf("unsupported order: %s", order)
	}
}

func matchesPerformerTitle(performer library.Performer, title string) bool {
	if strings.Contains(strings.ToLower(performer.Name), title) {
		return true
	}
	for _, alias := range performer.Aliases {
		if strings.Contains(strings.ToLower(alias), title) {
			return true
		}
	}
	return false
}

func numericID(id string) (int, bool) {
	parsed, err := strconv.Atoi(id)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func compareEntityIDs(leftID string, leftNumeric int, leftHasID bool, rightID string, rightNumeric int, rightHasID bool) int {
	if leftHasID && rightHasID {
		return cmp.Compare(leftNumeric, rightNumeric)
	}
	return strings.Compare(leftID, rightID)
}
