package filter

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func sceneFilter(objects map[string]any) gql.SavedFilterParts {
	return gql.SavedFilterParts{
		Id:            "1",
		Name:          "f",
		Mode:          gql.FilterModeScenes,
		Find_filter:   &gql.SavedFilterPartsFind_filterSavedFindFilterType{Sort: util.Ptr("title")},
		Object_filter: &objects,
	}
}

// convert runs the conversion and fails the test on a panic, which is
// what every malformed saved filter used to do.
func convert(t *testing.T, sf gql.SavedFilterParts) (f Filter, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SavedFilterToSceneFilter panicked: %v", r)
		}
	}()
	return SavedFilterToSceneFilter(context.Background(), sf)
}

func TestSavedFilterToSceneFilter_MalformedCriteriaReturnErrors(t *testing.T) {
	cases := []struct {
		name    string
		objects map[string]any
		wantErr string
	}{
		{"NOT_NULL string criterion", map[string]any{"title": map[string]any{"modifier": "NOT_NULL"}}, "criterion title"},
		{"NOT_NULL date criterion", map[string]any{"date": map[string]any{"modifier": "NOT_NULL"}}, "criterion date"},
		{"missing modifier", map[string]any{"title": map[string]any{"value": "x"}}, "criterion title: modifier"},
		{"non-object criterion value", map[string]any{"title": "x"}, "criterion title: value is string"},
		{"orientation without value", map[string]any{"orientation": map[string]any{"modifier": "INCLUDES"}}, "criterion orientation"},
		{"orientation with non-string item", map[string]any{"orientation": map[string]any{"value": []any{1.0}}}, "criterion orientation"},
		{"tags item without id", map[string]any{"tags": map[string]any{"modifier": "INCLUDES", "value": map[string]any{"items": []any{map[string]any{"label": "t"}}}}}, "criterion tags: value.items[0]"},
		{"tags excluded item without id", map[string]any{"tags": map[string]any{"modifier": "INCLUDES", "value": map[string]any{"excluded": []any{map[string]any{}}}}}, "criterion tags: value.excluded[0]"},
		{"performers item without id", map[string]any{"performers": map[string]any{"modifier": "INCLUDES", "value": []any{map[string]any{}}}}, "criterion performers: value[0]"},
		{"has_markers with a bool value", map[string]any{"has_markers": map[string]any{"modifier": "EQUALS", "value": true}}, "criterion has_markers: value is bool"},
		{"organized with a non-bool string", map[string]any{"organized": map[string]any{"value": "maybe"}}, "criterion organized"},
		{"timestamp without value", map[string]any{"created_at": map[string]any{"modifier": "GREATER_THAN"}}, "criterion created_at"},
		{"phash distance without value", map[string]any{"phash_distance": map[string]any{"modifier": "EQUALS"}}, "criterion phash_distance"},
		{"resolution without value", map[string]any{"resolution": map[string]any{"modifier": "EQUALS"}}, "criterion resolution"},
		{"captions without value", map[string]any{"captions": map[string]any{"modifier": "EQUALS"}}, "criterion captions"},
		{"int criterion with bool modifier", map[string]any{"duration": map[string]any{"modifier": true}}, "criterion duration: modifier is bool"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := convert(t, sceneFilter(c.objects))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not mention %q", err, c.wantErr)
			}
		})
	}
}

func TestSavedFilterToSceneFilter_NilFiltersAreTolerated(t *testing.T) {
	cases := []struct {
		name string
		sf   gql.SavedFilterParts
	}{
		{"nil Object_filter", gql.SavedFilterParts{Mode: gql.FilterModeScenes, Find_filter: &gql.SavedFilterPartsFind_filterSavedFindFilterType{Sort: util.Ptr("title")}}},
		{"nil Find_filter", gql.SavedFilterParts{Mode: gql.FilterModeScenes, Object_filter: &map[string]any{}}},
		{"both nil", gql.SavedFilterParts{Mode: gql.FilterModeScenes}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := convert(t, c.sf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(f.SceneFilter, gql.SceneFilterType{}) {
				t.Fatalf("expected an empty scene filter, got %+v", f.SceneFilter)
			}
			if f.FilterOpts.Per_page == nil || *f.FilterOpts.Per_page != -1 {
				t.Fatalf("expected per_page -1, got %v", f.FilterOpts.Per_page)
			}
			if c.sf.Find_filter == nil && (f.FilterOpts.Sort != nil || f.FilterOpts.Direction != nil) {
				t.Fatalf("nil find filter must leave sort and direction unset, got %v %v", f.FilterOpts.Sort, f.FilterOpts.Direction)
			}
		})
	}
}

func TestSavedFilterToSceneFilter_ConvertsWellFormedCriteria(t *testing.T) {
	dir := gql.SortDirectionEnumDesc
	sf := sceneFilter(map[string]any{
		"title":       map[string]any{"modifier": "INCLUDES", "value": "vr"},
		"duration":    map[string]any{"modifier": "GREATER_THAN", "value": map[string]any{"value": 60.0}},
		"date":        map[string]any{"modifier": "IS_NULL"},
		"tags":        map[string]any{"modifier": "INCLUDES_ALL", "value": map[string]any{"items": []any{map[string]any{"id": "7"}}, "excluded": []any{map[string]any{"id": "8"}}, "depth": -1.0}},
		"performers":  map[string]any{"modifier": "INCLUDES", "value": []any{map[string]any{"id": "3"}}},
		"orientation": map[string]any{"value": []any{"portrait"}},
		"has_markers": map[string]any{"value": "true"},
		"organized":   map[string]any{"value": "true"},
		"resolution":  map[string]any{"modifier": "GREATER_THAN", "value": "4k"},
		"captions":    map[string]any{"modifier": "INCLUDES", "value": "English"},
		"created_at":  map[string]any{"modifier": "GREATER_THAN", "value": map[string]any{"value": "2024-01-01"}},
		"duplicated":  map[string]any{"value": map[string]any{"phash": true}},
		"unknown":     map[string]any{"value": "ignored"},
	})
	sf.Find_filter = &gql.SavedFilterPartsFind_filterSavedFindFilterType{Sort: util.Ptr("random_123"), Direction: &dir}

	f, err := convert(t, sf)
	if err != nil {
		t.Fatal(err)
	}
	s := f.SceneFilter
	if s.Title == nil || s.Title.Value != "vr" || s.Title.Modifier != gql.CriterionModifierIncludes {
		t.Fatalf("title: %+v", s.Title)
	}
	if s.Duration == nil || s.Duration.Value != 60 {
		t.Fatalf("duration: %+v", s.Duration)
	}
	if s.Date == nil || s.Date.Modifier != gql.CriterionModifierIsNull || s.Date.Value != "" {
		t.Fatalf("date: %+v", s.Date)
	}
	if s.Tags == nil || len(s.Tags.Value) != 1 || s.Tags.Value[0] != "7" || len(s.Tags.Excludes) != 1 || s.Tags.Excludes[0] != "8" || s.Tags.Depth == nil || *s.Tags.Depth != -1 {
		t.Fatalf("tags: %+v", s.Tags)
	}
	if s.Performers == nil || len(s.Performers.Value) != 1 || s.Performers.Value[0] != "3" {
		t.Fatalf("performers: %+v", s.Performers)
	}
	if s.Orientation == nil || len(s.Orientation.Value) != 1 || s.Orientation.Value[0] != "PORTRAIT" {
		t.Fatalf("orientation: %+v", s.Orientation)
	}
	if s.Has_markers == nil || *s.Has_markers != "true" || s.Organized == nil || !*s.Organized {
		t.Fatalf("has_markers/organized: %v %v", s.Has_markers, s.Organized)
	}
	if s.Resolution == nil || s.Resolution.Value != gql.ResolutionEnumFourK {
		t.Fatalf("resolution: %+v", s.Resolution)
	}
	if s.Captions == nil || s.Captions.Value != "en" {
		t.Fatalf("captions: %+v", s.Captions)
	}
	if s.Created_at == nil || s.Created_at.Value != "2024-01-01" {
		t.Fatalf("created_at: %+v", s.Created_at)
	}
	if s.Duplicated == nil || s.Duplicated.Phash == nil || !*s.Duplicated.Phash {
		t.Fatalf("duplicated: %+v", s.Duplicated)
	}
	if f.FilterOpts.Sort == nil || *f.FilterOpts.Sort != "random" || f.FilterOpts.Direction == nil || *f.FilterOpts.Direction != dir {
		t.Fatalf("opts: %+v", f.FilterOpts)
	}
	if *sf.Find_filter.Sort != "random_123" {
		t.Fatal("conversion must not rewrite the saved filter's sort in place")
	}
}

func TestSavedFilterToSceneFilter_RejectsOtherModes(t *testing.T) {
	_, err := convert(t, gql.SavedFilterParts{Mode: gql.FilterModeImages})
	if err == nil || !strings.Contains(err.Error(), "unsupported filter mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}
