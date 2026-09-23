package library

import (
	"context"
	"encoding/json"

	"github.com/Khan/genqlient/graphql"
)

// fakeGraphQL answers each MakeRequest with the next JSON payload in order.
type fakeGraphQL struct {
	payloads []string
	calls    int
}

func (f *fakeGraphQL) MakeRequest(_ context.Context, _ *graphql.Request, resp *graphql.Response) error {
	payload := f.payloads[f.calls]
	f.calls++
	return json.Unmarshal([]byte(payload), resp.Data)
}
