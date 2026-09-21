package playa

import (
	"net/url"
	"testing"

	"stash-vr/internal/library"
)

func TestBuildCategoriesIncludesRandomMode(t *testing.T) {
	categories := buildCategories(
		[]library.SavedFilterSceneSet{{ID: "42", Name: "Favorites"}},
		[]library.Tag{{Id: "7", Name: "Bunny"}},
	)

	if len(categories) != 3 {
		t.Fatalf("expected 3 categories, got %d", len(categories))
	}
	if categories[0].ID != randomCategoryID || categories[0].Title != "Random" {
		t.Fatalf("expected first category to be Random mode, got %#v", categories[0])
	}
}

func TestBuildCategoryGroupsIncludesCustomFiltersGroup(t *testing.T) {
	groups := buildCategoryGroups(nil, nil)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].ID != "custom-filters" || groups[0].Title != "Custom Filters" {
		t.Fatalf("expected Custom Filters group, got %#v", groups[0])
	}
	if len(groups[0].Items) != 1 || groups[0].Items[0].ID != randomCategoryID {
		t.Fatalf("expected Custom Filters group to contain random category, got %#v", groups[0].Items)
	}
}

func TestParseVideoQueryTreatsRandomAsModifier(t *testing.T) {
	query, handled := parseVideoQuery(url.Values{
		"included-categories": []string{randomCategoryID + ",tag:7"},
	})

	if handled != nil {
		t.Fatalf("expected query to parse, got error %#v", handled)
	}
	if !query.Randomize {
		t.Fatal("expected randomize flag to be enabled")
	}
	if len(query.IncludedCategories) != 1 {
		t.Fatalf("expected 1 content category after filtering modifiers, got %d", len(query.IncludedCategories))
	}
	if query.IncludedCategories[0].Raw != "tag:7" {
		t.Fatalf("expected tag category to remain, got %#v", query.IncludedCategories[0])
	}
}

func TestParseVideoQueryRejectsExcludedRandomCategory(t *testing.T) {
	_, handled := parseVideoQuery(url.Values{
		"excluded-categories": []string{randomCategoryID},
	})

	if handled == nil {
		t.Fatal("expected excluded random category to be rejected")
	}
	if handled.Status.Message != "unsupported excluded category: "+randomCategoryID {
		t.Fatalf("unexpected error message: %q", handled.Status.Message)
	}
}

func TestParseCategoryKeyAcceptsRandomSystemCategory(t *testing.T) {
	key, err := parseCategoryKey(randomCategoryID)
	if err != nil {
		t.Fatalf("expected random category to parse, got %v", err)
	}
	if key.Kind != "system" || key.ID != randomCategoryKey {
		t.Fatalf("unexpected parsed key: %#v", key)
	}
}
