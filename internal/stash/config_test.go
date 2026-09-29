package stash

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

// minPlayStash answers UIConfiguration with the given ui JSON object, or fails.
type minPlayStash struct {
	ui  string
	err error
}

func (s minPlayStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	if s.err != nil {
		return s.err
	}
	if req.OpName != "UIConfiguration" {
		return errors.New("unexpected " + req.OpName)
	}
	return json.Unmarshal([]byte(`{"configuration":{"ui":`+s.ui+`}}`), resp.Data)
}

func TestGetMinPlayPercent(t *testing.T) {
	cases := []struct {
		name    string
		ui      string
		err     error
		want    float64
		wantErr bool
	}{
		{name: "number", ui: `{"minimumPlayPercent":50}`, want: 50},
		{name: "fractional number", ui: `{"minimumPlayPercent":12.5}`, want: 12.5},
		{name: "integer string", ui: `{"minimumPlayPercent":"50"}`, want: 50},
		{name: "fractional string", ui: `{"minimumPlayPercent":"12.5"}`, want: 12.5},
		{name: "unparsable string", ui: `{"minimumPlayPercent":"half"}`, want: 0},
		{name: "unsupported type", ui: `{"minimumPlayPercent":true}`, want: 0},
		{name: "missing", ui: `{}`, want: 0},
		{name: "null", ui: `{"minimumPlayPercent":null}`, want: 0},
		{name: "stash down", err: errors.New("boom"), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := GetMinPlayPercent(context.Background(), minPlayStash{ui: c.ui, err: c.err})
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
