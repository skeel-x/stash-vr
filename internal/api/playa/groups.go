package playa

import (
	"context"
	"fmt"
	"slices"
	"stash-vr/internal/library"
	"strings"
)

const (
	tagCategoryPrefix         = "tag:"
	savedFilterCategoryPrefix = "sf:"
	actorPrefix               = "actor:"
	studioPrefix              = "studio:"
	publishedStatusID         = "published"
)

type categoryKey struct {
	Raw  string
	Kind string
	ID   string
}

func parseCategoryList(raw string) ([]categoryKey, error) {
	parts, err := parseCSV(raw)
	if err != nil {
		return nil, err
	}
	keys := make([]categoryKey, 0, len(parts))
	for _, part := range parts {
		key, err := parseCategoryKey(part)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func parseCategoryKey(raw string) (categoryKey, error) {
	switch {
	case strings.HasPrefix(raw, tagCategoryPrefix):
		id := strings.TrimPrefix(raw, tagCategoryPrefix)
		if id == "" {
			return categoryKey{}, fmt.Errorf("invalid category ID: %s", raw)
		}
		return categoryKey{Raw: raw, Kind: "tag", ID: id}, nil
	case strings.HasPrefix(raw, savedFilterCategoryPrefix):
		id := strings.TrimPrefix(raw, savedFilterCategoryPrefix)
		if id == "" {
			return categoryKey{}, fmt.Errorf("invalid category ID: %s", raw)
		}
		return categoryKey{Raw: raw, Kind: "saved-filter", ID: id}, nil
	default:
		return categoryKey{}, fmt.Errorf("invalid category ID: %s", raw)
	}
}

func parseEntityID(raw string, prefix string, label string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if !strings.HasPrefix(raw, prefix) {
		return "", fmt.Errorf("invalid %s ID: %s", label, raw)
	}
	id := strings.TrimPrefix(raw, prefix)
	if id == "" {
		return "", fmt.Errorf("invalid %s ID: %s", label, raw)
	}
	return id, nil
}

func parseStatusList(raw string) ([]string, error) {
	statuses, err := parseCSV(raw)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if status != publishedStatusID {
			return nil, fmt.Errorf("unsupported status: %s", status)
		}
	}
	return statuses, nil
}

func parseCSV(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return nil, fmt.Errorf("malformed comma-separated list: %s", raw)
		}
		if slices.Contains(out, trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	return out, nil
}

func sliceToSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func intersectSets(left map[string]struct{}, right map[string]struct{}) map[string]struct{} {
	for key := range left {
		if _, ok := right[key]; !ok {
			delete(left, key)
		}
	}
	return left
}

func subtractSet(base map[string]struct{}, excluded map[string]struct{}) map[string]struct{} {
	for key := range excluded {
		delete(base, key)
	}
	return base
}

func setToSortedSlice(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func savedFilterLookup(filters []library.SavedFilterSceneSet) map[string]library.SavedFilterSceneSet {
	lookup := make(map[string]library.SavedFilterSceneSet, len(filters))
	for _, filter := range filters {
		lookup[filter.ID] = filter
	}
	return lookup
}

func resolveCategorySceneSet(ctx context.Context, libraryService *library.Service, category categoryKey, filters map[string]library.SavedFilterSceneSet) (map[string]struct{}, error) {
	switch category.Kind {
	case "saved-filter":
		filter, ok := filters[category.ID]
		if !ok {
			return map[string]struct{}{}, nil
		}
		return sliceToSet(filter.SceneIDs), nil
	case "tag":
		ids, err := libraryService.GetTagSceneIDs(ctx, category.ID)
		if err != nil {
			return nil, err
		}
		return sliceToSet(ids), nil
	default:
		return nil, fmt.Errorf("unsupported category kind: %s", category.Kind)
	}
}
