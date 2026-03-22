package playa

import "stash-vr/internal/library"

func buildCategories(savedFilters []library.SavedFilterSceneSet, tags []library.Tag) []CategoryListView {
	items := make([]CategoryListView, 0, len(savedFilters)+len(tags))
	for _, filter := range savedFilters {
		items = append(items, CategoryListView{ID: savedFilterCategoryPrefix + filter.ID, Title: filter.Name})
	}
	for _, tag := range tags {
		items = append(items, CategoryListView{ID: tagCategoryPrefix + tag.Id, Title: tag.Name})
	}
	return items
}

func buildCategoryGroups(savedFilters []library.SavedFilterSceneSet, tags []library.Tag) []CategoriesGroup {
	groups := make([]CategoriesGroup, 0, 2)
	if len(savedFilters) > 0 {
		items := make([]CategoryListView, 0, len(savedFilters))
		for _, filter := range savedFilters {
			items = append(items, CategoryListView{ID: savedFilterCategoryPrefix + filter.ID, Title: filter.Name})
		}
		groups = append(groups, CategoriesGroup{ID: "custom-filters", Title: "Custom Filters", Items: items})
	}
	if len(tags) > 0 {
		items := make([]CategoryListView, 0, len(tags))
		for _, tag := range tags {
			items = append(items, CategoryListView{ID: tagCategoryPrefix + tag.Id, Title: tag.Name})
		}
		groups = append(groups, CategoriesGroup{ID: "tags", Title: "Tags", Items: items})
	}
	return groups
}
