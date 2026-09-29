package library

import (
	"testing"

	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestVideoData_Description(t *testing.T) {
	cases := []struct {
		name string
		vd   VideoData
		want string
	}{
		{"no scene", VideoData{}, ""},
		{"no details", VideoData{SceneParts: &gql.SceneParts{Id: "1"}}, ""},
		{"empty details", VideoData{SceneParts: &gql.SceneParts{Id: "1", Details: util.Ptr("")}}, ""},
		{"details", VideoData{SceneParts: &gql.SceneParts{Id: "1", Details: util.Ptr("Two performers, one couch.")}}, "Two performers, one couch."},
	}
	for _, c := range cases {
		if got := c.vd.Description(); got != c.want {
			t.Errorf("%s: Description() = %q, want %q", c.name, got, c.want)
		}
	}
}
