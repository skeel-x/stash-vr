package playa

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Khan/genqlient/graphql"

	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
)

// pageScene is one scene pageStash knows.
type pageScene struct {
	title     string
	studio    string
	performer string
	playCount int
	createdAt string
}

// pageStash is a library whose every smart section lists every scene, so
// the index holds all of ids. Tag queries answer from tagged; everything
// else is empty. It counts the queries for all scene ids and for scenes.
type pageStash struct {
	mu     sync.Mutex
	scenes map[int]pageScene
	tagged map[string][]int
	allIds int
	finds  int
}

func (s *pageStash) ids() []int {
	ids := make([]int, 0, len(s.scenes))
	for id := range s.scenes {
		ids = append(ids, id)
	}
	return ids
}

func idList(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf(`{"id":"%d"}`, id)
	}
	return `{"findScenes":{"scenes":[` + strings.Join(parts, ",") + `]}}`
}

func (s *pageStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindSceneGroupings", "FindRecommendationScenes":
		payload = `{"findScenes":{"scenes":[]}}`
	case "FindAllSceneIds":
		s.allIds++
		payload = idList(s.ids())
	case "FindSceneIdsByFilter":
		raw, _ := json.Marshal(req.Variables)
		var in struct {
			SceneFilter struct {
				Tags *struct {
					Value []string `json:"value"`
				} `json:"tags"`
			} `json:"scene_filter"`
		}
		_ = json.Unmarshal(raw, &in)
		if in.SceneFilter.Tags != nil && len(in.SceneFilter.Tags.Value) > 0 {
			payload = idList(s.tagged[in.SceneFilter.Tags.Value[0]])
		} else {
			payload = idList(s.ids())
		}
	case "FindScenes":
		s.finds++
		raw, _ := json.Marshal(req.Variables)
		var in struct {
			SceneIDs []int `json:"scene_ids"`
		}
		_ = json.Unmarshal(raw, &in)
		var out []string
		for _, id := range in.SceneIDs {
			sc, ok := s.scenes[id]
			if !ok {
				continue
			}
			studio := "null"
			if sc.studio != "" {
				studio = fmt.Sprintf(`{"id":%q,"name":"Studio %s"}`, sc.studio, sc.studio)
			}
			performers := "[]"
			if sc.performer != "" {
				performers = fmt.Sprintf(`[{"id":%q,"name":"Performer %s"}]`, sc.performer, sc.performer)
			}
			created := sc.createdAt
			if created == "" {
				created = "2024-01-01T00:00:00Z"
			}
			out = append(out, fmt.Sprintf(`{"id":"%d","title":%q,"created_at":%q,"play_count":%d,"files":[],"tags":[],"studio":%s,"performers":%s,"paths":{"screenshot":"http://stash/scene/%d/screenshot"}}`,
				id, sc.title, created, sc.playCount, studio, performers, id))
		}
		payload = `{"findScenes":{"scenes":[` + strings.Join(out, ",") + `]}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func (s *pageStash) allIdQueries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allIds
}

// pageEnv loads plain settings and returns a handler over five scenes:
// S1 to S5, S1 and S2 of studio "a" (tag 7), S3 played twice by performer
// "p", S5 created last.
func pageEnv(t *testing.T) (httpHandler, *pageStash) {
	t.Helper()
	if err := config.Load(config.ApplicationConfig{
		ListenAddress: ":9666", StashGraphQLUrl: "http://stash:9999/graphql", LogLevel: "info",
		ExcludeSortName: "hidden", SmartSectionSize: 50, ConfigPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	st := &pageStash{
		scenes: map[int]pageScene{
			1: {title: "S1", studio: "a"},
			2: {title: "S2", studio: "a"},
			3: {title: "S3", performer: "p", playCount: 2},
			4: {title: "S4"},
			5: {title: "S5", createdAt: "2025-01-01T00:00:00Z"},
		},
		tagged: map[string][]int{"7": {1, 2}},
	}
	return httpHandler{libraryService: library.NewService(st)}, st
}

func pageIDs(page Page[VideoListView]) []string {
	ids := make([]string, len(page.Content))
	for i, item := range page.Content {
		ids[i] = item.ID
	}
	return ids
}

func TestBuildVideoPage_BuildsTheRequestedPageOnly(t *testing.T) {
	h, st := pageEnv(t)

	for _, c := range []struct {
		pageIndex int
		want      []string
	}{
		{0, []string{"1", "2"}},
		{1, []string{"3", "4"}},
		{2, []string{"5"}},
		{7, []string{}},
	} {
		page, err := h.buildVideoPage(context.Background(), videoQuery{PageIndex: c.pageIndex, PageSize: 2, Order: "title", Direction: "asc"}, "https://vr.example")
		if err != nil {
			t.Fatal(err)
		}
		if got := pageIDs(page); strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("page %d: got %v, want %v", c.pageIndex, got, c.want)
		}
		if page.ItemTotal != 5 || page.PageTotal != 3 || page.PageIndex != c.pageIndex || page.PageSize != 2 {
			t.Errorf("page %d: got totals %d/%d index %d size %d", c.pageIndex, page.ItemTotal, page.PageTotal, page.PageIndex, page.PageSize)
		}
		if len(page.Content) != len(c.want) {
			t.Errorf("page %d: %d views built for %d rows", c.pageIndex, len(page.Content), len(c.want))
		}
	}
	if st.allIdQueries() != 0 {
		t.Fatalf("the video page must not ask Stash for every scene id, got %d queries", st.allIdQueries())
	}
}

func TestBuildVideoPage_Filters(t *testing.T) {
	h, _ := pageEnv(t)
	tag7 := categoryKey{Raw: "tag:7", Kind: "tag", ID: "7"}
	for name, c := range map[string]struct {
		query videoQuery
		want  []string
	}{
		"title is case insensitive": {videoQuery{Title: "s3"}, []string{"3"}},
		"actor":                     {videoQuery{ActorID: "p"}, []string{"3"}},
		"studio":                    {videoQuery{StudioID: "a"}, []string{"1", "2"}},
		"included tag":              {videoQuery{IncludedCategories: []categoryKey{tag7}}, []string{"1", "2"}},
		"excluded tag":              {videoQuery{ExcludedCategories: []categoryKey{tag7}}, []string{"3", "4", "5"}},
		"unknown saved filter":      {videoQuery{IncludedCategories: []categoryKey{{Raw: "sf:99", Kind: "saved-filter", ID: "99"}}}, []string{}},
		"published only":            {videoQuery{IncludedStatuses: []string{publishedStatusID}}, []string{"1", "2", "3", "4", "5"}},
		"published excluded":        {videoQuery{ExcludedStatuses: []string{publishedStatusID}}, []string{}},
		"popularity":                {videoQuery{Order: "popularity", Direction: "desc"}, []string{"3", "1", "2", "4", "5"}},
		"newest first":              {videoQuery{Order: "release_date", Direction: "desc"}, []string{"5", "1", "2", "3", "4"}},
		"title descending":          {videoQuery{Order: "title", Direction: "desc"}, []string{"5", "4", "3", "2", "1"}},
	} {
		t.Run(name, func(t *testing.T) {
			q := c.query
			q.PageSize = 10
			if q.Order == "" {
				q.Order, q.Direction = "title", "asc"
			}
			page, err := h.buildVideoPage(context.Background(), q, "https://vr.example")
			if err != nil {
				t.Fatal(err)
			}
			if got := pageIDs(page); strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestBuildVideoPage_PosterURLsCarryTheCoverFingerprint(t *testing.T) {
	h, _ := pageEnv(t)
	cfg := config.Application()
	cfg.CoverBadges = config.CoverBadges{Quality: true}
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(coverbadge.ResetCache)

	page, err := h.buildVideoPage(context.Background(), videoQuery{PageSize: 10, Order: "title", Direction: "asc"}, "https://vr.example")
	if err != nil {
		t.Fatal(err)
	}

	want := "https://vr.example/api/playa/v2/poster/1" + coverbadge.CurrentURLQuery()
	if got := page.Content[0].PreviewImage; got == nil || *got != want || !strings.Contains(want, "?b=") {
		t.Fatalf("expected %q, got %v", want, got)
	}
}

func TestBuildVideoPage_RandomPagesAgree(t *testing.T) {
	h, _ := pageEnv(t)
	random := videoQuery{PageSize: 2, Order: "title", Direction: "asc", Randomize: true}

	var seen []string
	for pageIndex := 0; pageIndex < 3; pageIndex++ {
		q := random
		q.PageIndex = pageIndex
		page, err := h.buildVideoPage(context.Background(), q, "https://vr.example")
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, pageIDs(page)...)
	}
	sorted := append([]string(nil), seen...)
	slices.Sort(sorted)
	if strings.Join(sorted, ",") != "1,2,3,4,5" {
		t.Fatalf("random pages must list every scene once, got %v", seen)
	}

	again, err := h.buildVideoPage(context.Background(), random, "https://vr.example")
	if err != nil {
		t.Fatal(err)
	}
	if got := pageIDs(again); strings.Join(got, ",") != strings.Join(seen[:2], ",") {
		t.Fatalf("the same page asked again must not change while the index stands, got %v, want %v", got, seen[:2])
	}
	if h.libraryService.IndexBuiltAt().IsZero() {
		t.Fatal("expected the index build time to seed the order")
	}
}

func TestShuffleVideoData(t *testing.T) {
	scenes := func(ids ...string) []*library.VideoData {
		out := make([]*library.VideoData, len(ids))
		for i, id := range ids {
			out[i] = &library.VideoData{SceneParts: &gql.SceneParts{Id: id}}
		}
		return out
	}
	order := func(items []*library.VideoData) string {
		ids := make([]string, len(items))
		for i, vd := range items {
			ids[i] = vd.Id()
		}
		return strings.Join(ids, ",")
	}
	ids := []string{"12", "3", "7", "1", "20", "5", "9", "15", "2", "11", "4", "8"}
	reversed := slices.Clone(ids)
	slices.Reverse(reversed)

	a, b := scenes(ids...), scenes(reversed...)
	shuffleVideoData(a, 42)
	shuffleVideoData(b, 42)
	if order(a) != order(b) {
		t.Fatalf("the same seed must give the same order whatever the input order: %s vs %s", order(a), order(b))
	}
	c := scenes(ids...)
	shuffleVideoData(c, 43)
	if order(c) == order(a) {
		t.Fatal("another seed must give another order")
	}
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(x, y string) int {
		l, _ := numericID(x)
		r, _ := numericID(y)
		return cmp.Compare(l, r)
	})
	if order(a) == strings.Join(sorted, ",") {
		t.Fatal("expected a shuffle, got id order")
	}

	one := scenes("1")
	shuffleVideoData(one, 1)
	if order(one) != "1" {
		t.Fatal("a single item stays")
	}
}

func TestBuildVideoPage_StashDownIsAnError(t *testing.T) {
	h := httpHandler{libraryService: library.NewService(&panickingGraphQL{})}
	h.libraryService.SetStashClient(failingGraphQL{})

	if _, err := h.buildVideoPage(context.Background(), videoQuery{PageSize: 10, Order: "title"}, "https://vr.example"); err == nil {
		t.Fatal("expected the page to fail while Stash is down")
	}
}

type failingGraphQL struct{}

func (failingGraphQL) MakeRequest(context.Context, *graphql.Request, *graphql.Response) error {
	return fmt.Errorf("stash unreachable")
}

func TestPageBounds(t *testing.T) {
	for _, c := range []struct {
		total, index, size    int
		start, end, pageTotal int
	}{
		{0, 0, 10, 0, 0, 1},
		{5, 0, 2, 0, 2, 3},
		{5, 2, 2, 4, 5, 3},
		{5, 3, 2, 5, 5, 3},
		{4, 0, 4, 0, 4, 1},
		{4, 1, 4, 4, 4, 1},
		// A page index near MaxInt must give an empty page, not overflow
		// the multiplication into a negative slice start.
		{5, math.MaxInt, 2, 5, 5, 3},
		{5, math.MaxInt / 2, 2, 5, 5, 3},
		{5, math.MaxInt, 1, 5, 5, 5},
		{0, math.MaxInt, 1000, 0, 0, 1},
	} {
		start, end, pageTotal := pageBounds(c.total, c.index, c.size)
		if start != c.start || end != c.end || pageTotal != c.pageTotal {
			t.Errorf("pageBounds(%d, %d, %d) = %d, %d, %d; want %d, %d, %d", c.total, c.index, c.size, start, end, pageTotal, c.start, c.end, c.pageTotal)
		}
	}
	page := paginate([]int{1, 2, 3}, 1, 2)
	if len(page.Content) != 1 || page.Content[0] != 3 || page.ItemTotal != 3 || page.PageTotal != 2 {
		t.Fatalf("unexpected page %+v", page)
	}
}

func TestPaginate_MaxIntPageIndexIsEmpty(t *testing.T) {
	page := paginate([]int{1, 2, 3}, math.MaxInt, 2)
	if len(page.Content) != 0 || page.ItemTotal != 3 || page.PageTotal != 2 || page.PageIndex != math.MaxInt || page.PageSize != 2 {
		t.Fatalf("unexpected page %+v", page)
	}
}
