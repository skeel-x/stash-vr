package stash

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

func TestFrontPageFilterIds_SkipsMalformedEntries(t *testing.T) {
	cases := []struct {
		name    string
		content any
		want    []string
	}{
		{"nil content", nil, nil},
		{"content is not a list", map[string]any{"x": 1}, nil},
		{"string and numeric ids", []any{
			map[string]any{"__typename": "SavedFilter", "savedFilterId": "3"},
			map[string]any{"__typename": "SavedFilter", "savedFilterId": 4.0},
		}, []string{"3", "4"}},
		{"entry is not an object", []any{"nope", map[string]any{"__typename": "SavedFilter", "savedFilterId": "5"}}, []string{"5"}},
		{"missing __typename", []any{map[string]any{"savedFilterId": "6"}}, []string{}},
		{"__typename is not a string", []any{map[string]any{"__typename": 1.0, "savedFilterId": "6"}}, []string{}},
		{"unsupported type", []any{map[string]any{"__typename": "CustomFilter", "savedFilterId": "6"}}, []string{}},
		{"missing savedFilterId", []any{map[string]any{"__typename": "SavedFilter"}}, []string{}},
		{"savedFilterId wrong type", []any{map[string]any{"__typename": "SavedFilter", "savedFilterId": true}}, []string{}},
		{"empty savedFilterId", []any{map[string]any{"__typename": "SavedFilter", "savedFilterId": ""}}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := frontPageFilterIds(context.Background(), c.content)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}

type uiStash struct{ ui string }

func (u *uiStash) MakeRequest(_ context.Context, _ *graphql.Request, resp *graphql.Response) error {
	return json.Unmarshal([]byte(`{"configuration":{"ui":`+u.ui+`}}`), resp.Data)
}

func TestFindSavedFilterIdsByFrontPage_ReadsUiConfiguration(t *testing.T) {
	client := &uiStash{ui: `{"frontPageContent":[{"__typename":"SavedFilter","savedFilterId":7},{"__typename":"SavedFilter"},{"__typename":"SavedFilter","savedFilterId":"8"}]}`}

	got, err := FindSavedFilterIdsByFrontPage(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"7", "8"}) {
		t.Fatalf("got %v", got)
	}
}
