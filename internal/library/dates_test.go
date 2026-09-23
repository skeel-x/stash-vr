package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

func TestAcceptScrapedDate(t *testing.T) {
	r := func(title, date string) scraped { return scraped{Title: title, Date: date} }
	cases := []struct {
		name    string
		title   string
		stem    string
		results []scraped
		want    string
		matched bool
		write   bool
		ok      bool
	}{
		{"single result with any title", "My Scene", "file", []scraped{r("Something Else", "2020-01-02")}, "2020-01-02", false, false, true},
		{"single matching result", "My Scene", "file", []scraped{r("My Scene", "2020-01-02")}, "2020-01-02", true, true, true},
		{"single matching short title", "Scene1", "file", []scraped{r("scene 1", "2020-01-02")}, "2020-01-02", true, false, true},
		{"exact title among many", "My Scene!", "file", []scraped{r("Other", "2019-01-01"), r("my scene", "2021-03-04"), r("Third", "2018-01-01")}, "2021-03-04", true, true, true},
		{"stem match among many", "", "Studio - My_Scene [VR]", []scraped{r("Other", "2019-01-01"), r("Studio My Scene VR", "2022-05-06")}, "2022-05-06", true, true, true},
		{"no match among many", "My Scene", "file", []scraped{r("Other", "2019-01-01"), r("Another", "2018-01-01")}, "", false, false, false},
		{"matched but year only", "My Scene", "file", []scraped{r("Other", "2019-01-01"), r("My Scene", "2018")}, "", false, false, false},
		{"single result with a year only", "My Scene", "file", []scraped{r("My Scene", "2018")}, "", false, false, false},
		{"epoch date is junk", "My Scene", "file", []scraped{r("My Scene", "1970-01-01")}, "", false, false, false},
		{"no results", "My Scene", "file", nil, "", false, false, false},
		{"empty titles never match", "", "", []scraped{r("", "2019-01-01"), r("", "2018-01-01")}, "", false, false, false},
	}
	for _, c := range cases {
		got, matched, write, ok := acceptScrapedDate(c.title, c.stem, c.results)
		if got != c.want || matched != c.matched || write != c.write || ok != c.ok {
			t.Errorf("%s: got %q,%v,%v,%v want %q,%v,%v,%v", c.name, got, matched, write, ok, c.want, c.matched, c.write, c.ok)
		}
	}
}

func TestDateStore_RoundTripAndMissExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dates.json")
	now := time.Now()
	store := loadDateStore(path)
	store.put("1", dateEntry{Date: "2020-01-02", Source: "stashdb", Checked: now})
	store.put("2", dateEntry{Checked: now.Add(-24 * time.Hour)})
	store.put("3", dateEntry{Checked: now.Add(-31 * 24 * time.Hour)})
	if err := store.flush(); err != nil {
		t.Fatal(err)
	}

	loaded := loadDateStore(path)
	e, ok := loaded.get("1")
	if !ok || e.Date != "2020-01-02" || e.Source != "stashdb" {
		t.Fatalf("expected the hit back, got %+v %v", e, ok)
	}
	if _, ok := loaded.get("9"); ok {
		t.Fatal("unknown id must not be found")
	}
	for id, want := range map[string]bool{"1": true, "2": true, "3": false} {
		e, _ := loaded.get(id)
		if got := e.settled(now); got != want {
			t.Errorf("settled(%s) = %v want %v", id, got, want)
		}
	}
}

func TestDateStore_MissingOrBrokenFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if s := loadDateStore(filepath.Join(dir, "absent.json")); len(s.entries) != 0 {
		t.Fatal("expected an empty store for a missing file")
	}
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := loadDateStore(broken); len(s.entries) != 0 {
		t.Fatal("expected an empty store for an unparsable file")
	}
	// An empty path keeps the store in memory only.
	s := loadDateStore("")
	s.put("1", dateEntry{Date: "2020-01-01"})
	if err := s.flush(); err != nil {
		t.Fatalf("in-memory flush: %v", err)
	}
}

func TestReleaseDate_Precedence(t *testing.T) {
	stash := VideoData{SceneParts: &gql.SceneParts{Id: "1", Date: util.Ptr("2019-01-01")}, releaseDate: "2020-01-01"}
	if got := stash.ReleaseDate(); got != "2019-01-01" {
		t.Fatalf("Stash date must win, got %q", got)
	}
	looked := VideoData{SceneParts: &gql.SceneParts{Id: "1", Date: util.Ptr("")}, releaseDate: "2020-01-01"}
	if got := looked.ReleaseDate(); got != "2020-01-01" {
		t.Fatalf("store date next, got %q", got)
	}
	if got := (VideoData{SceneParts: &gql.SceneParts{Id: "1"}}).ReleaseDate(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := (VideoData{}).ReleaseDate(); got != "" {
		t.Fatalf("expected empty for no scene, got %q", got)
	}
}

// dateScene is one scene the dateStash knows.
type dateScene struct {
	title, date, basename string
}

// dateStash serves an "All" index of its scenes, one stash-box, scrape
// results per scene id, and records scrapes and date write-backs.
type dateStash struct {
	mu        sync.Mutex
	scenes    map[string]dateScene
	results   map[string]string // scene id -> scrapeSingleScene JSON array
	boxes     string            // stashBoxes JSON array
	scrapeErr error
	scrapes   []string // "index:id" per ScrapeSceneDate
	updates   []string // "id=date" per SceneUpdateDate
}

func (f *dateStash) vars(req *graphql.Request, into any) error {
	raw, err := json.Marshal(req.Variables)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func (f *dateStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var payload string
	switch req.OpName {
	case "FindSavedSceneFilters":
		payload = `{"findSavedFilters":[]}`
	case "FindAllTags":
		payload = `{"findTags":{"tags":[]}}`
	case "FindAllSceneIds":
		ids := make([]string, 0, len(f.scenes))
		for id := range f.scenes {
			ids = append(ids, fmt.Sprintf(`{"id":%q}`, id))
		}
		slices.Sort(ids)
		payload = `{"findScenes":{"scenes":[` + strings.Join(ids, ",") + `]}}`
	case "FindScenes":
		var in struct {
			SceneIDs []int `json:"scene_ids"`
		}
		if err := f.vars(req, &in); err != nil {
			return err
		}
		out := []map[string]any{}
		for _, n := range in.SceneIDs {
			id := fmt.Sprint(n)
			sc, ok := f.scenes[id]
			if !ok {
				continue
			}
			m := map[string]any{"id": id, "title": sc.title, "created_at": "2024-01-01T00:00:00Z", "tags": []any{},
				"files": []map[string]any{{"basename": sc.basename, "duration": 100, "path": "/v/" + sc.basename, "height": 1080, "video_codec": "h264"}}}
			if sc.date != "" {
				m["date"] = sc.date
			}
			out = append(out, m)
		}
		b, _ := json.Marshal(map[string]any{"findScenes": map[string]any{"scenes": out}})
		payload = string(b)
	case "StashBoxes":
		boxes := f.boxes
		if boxes == "" {
			boxes = `[{"name":"stashdb","endpoint":"https://stashdb.org/graphql"}]`
		}
		payload = `{"configuration":{"general":{"stashBoxes":` + boxes + `}}}`
	case "ScrapeSceneDate":
		var in struct {
			Index int    `json:"index"`
			Id    string `json:"id"`
		}
		if err := f.vars(req, &in); err != nil {
			return err
		}
		f.scrapes = append(f.scrapes, fmt.Sprintf("%d:%s", in.Index, in.Id))
		if f.scrapeErr != nil {
			return f.scrapeErr
		}
		res := f.results[in.Id]
		if res == "" {
			res = "[]"
		}
		payload = `{"scrapeSingleScene":` + res + `}`
	case "SceneUpdateDate":
		var in struct {
			Id   string  `json:"id"`
			Date *string `json:"date"`
		}
		if err := f.vars(req, &in); err != nil {
			return err
		}
		f.updates = append(f.updates, in.Id+"="+util.FirstNonEmpty(in.Date))
		if sc, ok := f.scenes[in.Id]; ok && in.Date != nil {
			sc.date = *in.Date
			f.scenes[in.Id] = sc
		}
		payload = `{"sceneUpdate":{"id":"` + in.Id + `"}}`
	default:
		payload = `{}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func (f *dateStash) scrapeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.scrapes)
}

func (f *dateStash) updateCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.updates)
}

// loadDateConfig loads settings with only the "All" fallback section and the
// given date settings.
func loadDateConfig(t *testing.T, lookup, writeback bool) {
	t.Helper()
	err := config.Load(config.ApplicationConfig{
		ListenAddress:    ":9666",
		StashGraphQLUrl:  "http://stash:9999/graphql",
		FavoriteTag:      "FAVORITE",
		LogLevel:         "info",
		ExcludeSortName:  "hidden",
		SmartSectionSize: 50,
		ConfigPath:       t.TempDir(),
		DateLookup:       lookup,
		DateWriteback:    writeback,
		Filters: []config.Filter{
			{ID: "smart:continue", Disabled: true},
			{ID: "smart:recent", Disabled: true},
			{ID: "smart:random", Disabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func datesPath() string {
	return filepath.Join(config.Application().ConfigPath, "dates.json")
}

func TestGetScene_FillsReleaseDateFromStore(t *testing.T) {
	loadDateConfig(t, true, false)
	if err := os.WriteFile(datesPath(), []byte(`{"1":{"date":"2020-05-06","source":"stashdb","checked":"2026-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewService(&dateStash{scenes: map[string]dateScene{"1": {title: "One", basename: "one.mp4"}}})

	vd, err := svc.GetScene(context.Background(), "1", false)
	if err != nil {
		t.Fatal(err)
	}
	if vd.ReleaseDate() != "2020-05-06" || svc.LookedUpDate("1") != "2020-05-06" {
		t.Fatalf("expected the stored date, got %q / %q", vd.ReleaseDate(), svc.LookedUpDate("1"))
	}
	if svc.LookedUpDate("2") != "" {
		t.Fatal("expected no date for an unknown scene")
	}
}

func TestLookupDate_StoresHitWithoutWriteback(t *testing.T) {
	loadDateConfig(t, true, false)
	stash := &dateStash{
		scenes:  map[string]dateScene{"1": {title: "My Scene", basename: "one.mp4"}},
		results: map[string]string{"1": `[{"title":"My Scene","date":"2020-01-02"}]`},
	}
	svc := NewService(stash)

	svc.lookupDate(context.Background(), "1")

	if got := svc.LookedUpDate("1"); got != "2020-01-02" {
		t.Fatalf("expected the hit stored, got %q", got)
	}
	e, _ := svc.dates().get("1")
	if e.Source != "stashdb" {
		t.Fatalf("expected the box name as source, got %+v", e)
	}
	if len(stash.updateCalls()) != 0 {
		t.Fatalf("no write-back expected, got %v", stash.updateCalls())
	}
	vd, err := svc.GetScene(context.Background(), "1", false)
	if err != nil || vd.ReleaseDate() != "2020-01-02" {
		t.Fatalf("expected the cached scene to carry the date, got %v %v", vd, err)
	}
	if err := svc.dates().flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(datesPath())
	if err != nil || !strings.Contains(string(data), `"2020-01-02"`) {
		t.Fatalf("expected dates.json to hold the hit, got %s %v", data, err)
	}
}

func TestLookupDate_WritesBackWhenEnabled(t *testing.T) {
	loadDateConfig(t, true, true)
	stash := &dateStash{
		scenes:  map[string]dateScene{"1": {title: "My Scene", basename: "one.mp4"}},
		results: map[string]string{"1": `[{"title":"My Scene","date":"2020-01-02"}]`},
	}
	svc := NewService(stash)

	svc.lookupDate(context.Background(), "1")

	if got := stash.updateCalls(); !slices.Equal(got, []string{"1=2020-01-02"}) {
		t.Fatalf("expected one write-back, got %v", got)
	}
	vd, err := svc.GetScene(context.Background(), "1", false)
	if err != nil || vd.SceneParts.Date == nil || *vd.SceneParts.Date != "2020-01-02" {
		t.Fatalf("expected the cache refreshed with Stash's new date, got %+v %v", vd.SceneParts.Date, err)
	}
}

func TestLookupDate_MissAndErrors(t *testing.T) {
	loadDateConfig(t, true, false)
	stash := &dateStash{
		scenes: map[string]dateScene{
			"1": {title: "My Scene", basename: "one.mp4"},
			"2": {title: "Dated", date: "2019-01-01", basename: "two.mp4"},
		},
		results: map[string]string{"1": `[{"title":"A","date":"2020-01-02"},{"title":"B","date":"2020-01-03"}]`},
		boxes:   `[{"name":"first","endpoint":"https://a/graphql"},{"name":"second","endpoint":"https://b/graphql"}]`,
	}
	svc := NewService(stash)

	svc.lookupDate(context.Background(), "1")
	e, ok := svc.dates().get("1")
	if !ok || e.Date != "" || !e.settled(time.Now()) {
		t.Fatalf("expected a recorded miss, got %+v %v", e, ok)
	}
	if got := stash.scrapeCalls(); !slices.Equal(got, []string{"0:1", "1:1"}) {
		t.Fatalf("expected both boxes asked in order, got %v", got)
	}

	svc.lookupDate(context.Background(), "2")
	if got := stash.scrapeCalls(); len(got) != 2 {
		t.Fatalf("a dated scene must never be looked up, got %v", got)
	}

	// When every box fails the scene is stored as failed, not as a miss.
	stash.mu.Lock()
	stash.scrapeErr = errors.New("box down")
	stash.mu.Unlock()
	loadDateConfig(t, true, false)
	svc2 := NewService(stash)
	svc2.lookupDate(context.Background(), "1")
	e, ok = svc2.dates().get("1")
	if !ok || !e.Failed || e.Date != "" || e.Checked.IsZero() {
		t.Fatalf("expected a failed entry, got %+v %v", e, ok)
	}
}

func TestLookupDate_UnmatchedSingleResultIsNotWrittenBack(t *testing.T) {
	loadDateConfig(t, true, true)
	stash := &dateStash{
		scenes:  map[string]dateScene{"1": {title: "My Long Scene", basename: "one.mp4"}},
		results: map[string]string{"1": `[{"title":"Something Else Entirely","date":"2020-01-02"}]`},
	}
	svc := NewService(stash)

	svc.lookupDate(context.Background(), "1")

	e, ok := svc.dates().get("1")
	if !ok || e.Date != "2020-01-02" || e.Matched {
		t.Fatalf("expected an unmatched hit stored, got %+v %v", e, ok)
	}
	if got := svc.LookedUpDate("1"); got != "2020-01-02" {
		t.Fatalf("expected the date for display, got %q", got)
	}
	if got := stash.updateCalls(); len(got) != 0 {
		t.Fatalf("an unmatched guess must not be written back, got %v", got)
	}
}

func TestLookupDate_MatchedLongTitleIsWrittenBack(t *testing.T) {
	loadDateConfig(t, true, true)
	stash := &dateStash{
		scenes:  map[string]dateScene{"1": {title: "", basename: "Studio - My_Scene [VR].mp4"}},
		results: map[string]string{"1": `[{"title":"Other","date":"2019-01-01"},{"title":"Studio My Scene VR","date":"2022-05-06"}]`},
	}
	svc := NewService(stash)

	svc.lookupDate(context.Background(), "1")

	e, _ := svc.dates().get("1")
	if !e.Matched || e.Date != "2022-05-06" {
		t.Fatalf("expected a matched hit, got %+v", e)
	}
	if got := stash.updateCalls(); !slices.Equal(got, []string{"1=2022-05-06"}) {
		t.Fatalf("expected one write-back, got %v", got)
	}
}

func TestLookupDate_MatchedGenericTitleIsNotWrittenBack(t *testing.T) {
	loadDateConfig(t, true, true)
	stash := &dateStash{
		scenes:  map[string]dateScene{"1": {title: "Intro", basename: "intro.mp4"}},
		results: map[string]string{"1": `[{"title":"intro","date":"2020-01-02"}]`},
	}
	svc := NewService(stash)

	svc.lookupDate(context.Background(), "1")

	e, _ := svc.dates().get("1")
	if !e.Matched || e.Date != "2020-01-02" {
		t.Fatalf("expected a matched hit stored, got %+v", e)
	}
	if got := stash.updateCalls(); len(got) != 0 {
		t.Fatalf("a short generic title must not be written back, got %v", got)
	}
}

func TestDateEntry_FailedBackoff(t *testing.T) {
	now := time.Now()
	if !(dateEntry{Failed: true, Checked: now.Add(-5 * time.Hour)}).settled(now) {
		t.Fatal("a failed entry must be settled within 6 hours")
	}
	if (dateEntry{Failed: true, Checked: now.Add(-7 * time.Hour)}).settled(now) {
		t.Fatal("a failed entry must be retried after 6 hours")
	}
	if !(dateEntry{Checked: now.Add(-7 * time.Hour)}).settled(now) {
		t.Fatal("a miss stays settled for 30 days")
	}
}

func TestLookupDate_FailedEntryRetriedAfterBackoff(t *testing.T) {
	loadDateConfig(t, true, false)
	stash := &dateStash{
		scenes:  map[string]dateScene{"1": {title: "My Scene", basename: "one.mp4"}},
		results: map[string]string{"1": `[{"title":"My Scene","date":"2020-01-02"}]`},
	}
	svc := NewService(stash)
	svc.dates().put("1", dateEntry{Failed: true, Checked: time.Now().Add(-time.Hour)})
	if _, err := svc.GetScenes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := svc.dateCandidates(context.Background()); len(got) != 0 {
		t.Fatalf("a recent failure must not be retried, got %v", got)
	}
	svc.RequestDate("1")
	if _, ok := svc.sweeper.popPriority(); ok {
		t.Fatal("a recent failure must not be queued")
	}

	svc.dates().put("1", dateEntry{Failed: true, Checked: time.Now().Add(-7 * time.Hour)})
	if got := svc.dateCandidates(context.Background()); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("an old failure must be retried, got %v", got)
	}
	svc.lookupDate(context.Background(), "1")
	e, _ := svc.dates().get("1")
	if e.Failed || e.Date != "2020-01-02" {
		t.Fatalf("expected the retry to replace the failure, got %+v", e)
	}
}

func TestDateStats_CountsUndatedScenes(t *testing.T) {
	loadDateConfig(t, true, false)
	old := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)
	content := fmt.Sprintf(`{"1":{"date":"2020-01-01","checked":%q},"2":{"date":"","checked":%q},"3":{"date":"","checked":%q},"6":{"date":"","checked":%q,"failed":true}}`, recent, recent, old, recent)
	if err := os.WriteFile(datesPath(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stash := &dateStash{scenes: map[string]dateScene{
		"1": {title: "found", basename: "1.mp4"},
		"2": {title: "missing", basename: "2.mp4"},
		"3": {title: "expired miss", basename: "3.mp4"},
		"4": {title: "never asked", basename: "4.mp4"},
		"5": {title: "dated", date: "2019-01-01", basename: "5.mp4"},
		"6": {title: "failed recently", basename: "6.mp4"},
	}}
	svc := NewService(stash)
	if _, err := svc.GetScenes(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := svc.DateStats()

	if got != (DateStats{Found: 1, Missing: 1, Unchecked: 3}) {
		t.Fatalf("got %+v", got)
	}
}

// startSweeper runs the sweeper with a tiny interval until the test ends.
func startSweeper(t *testing.T, svc *Service) {
	t.Helper()
	prev := dateSweepInterval
	dateSweepInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.runDateSweeper(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		dateSweepInterval = prev
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestDateSweeper_LooksUpUndatedScenesOnly(t *testing.T) {
	loadDateConfig(t, true, false)
	stash := &dateStash{
		scenes: map[string]dateScene{
			"1": {title: "Undated", basename: "1.mp4"},
			"2": {title: "Dated", date: "2019-01-01", basename: "2.mp4"},
		},
		results: map[string]string{"1": `[{"title":"Undated","date":"2021-02-03"}]`},
	}
	svc := NewService(stash)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatal(err)
	}
	startSweeper(t, svc)

	waitFor(t, "scene 1 looked up", func() bool { return svc.LookedUpDate("1") == "2021-02-03" })
	waitFor(t, "dates.json written", func() bool {
		data, err := os.ReadFile(datesPath())
		return err == nil && strings.Contains(string(data), "2021-02-03")
	})
	for _, call := range stash.scrapeCalls() {
		if strings.HasSuffix(call, ":2") {
			t.Fatalf("dated scene 2 was scraped: %v", stash.scrapeCalls())
		}
	}

	// A rebuilt index restarts the walk; the settled scene is not asked again.
	n := len(stash.scrapeCalls())
	svc.ResetSections()
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := stash.scrapeCalls(); len(got) != n {
		t.Fatalf("expected no new scrapes after a rebuild, got %v", got)
	}
}

func TestDateSweeper_RequestedSceneGoesFirst(t *testing.T) {
	loadDateConfig(t, true, false)
	stash := &dateStash{scenes: map[string]dateScene{
		"1": {title: "One", basename: "1.mp4"},
		"2": {title: "Two", basename: "2.mp4"},
		"5": {title: "Five", basename: "5.mp4"},
	}}
	svc := NewService(stash)
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.RequestDate("5")
	svc.RequestDate("5")
	startSweeper(t, svc)

	waitFor(t, "three lookups", func() bool { return len(stash.scrapeCalls()) >= 3 })
	got := stash.scrapeCalls()
	if got[0] != "0:5" {
		t.Fatalf("expected the requested scene first, got %v", got)
	}
	if slices.Index(got[1:], "0:5") >= 0 {
		t.Fatalf("a requested scene is looked up once, got %v", got)
	}
}

func TestDateSweeper_IdleWhenLookupOff(t *testing.T) {
	loadDateConfig(t, false, false)
	stash := &dateStash{scenes: map[string]dateScene{"1": {title: "One", basename: "1.mp4"}}}
	svc := NewService(stash)
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.RequestDate("1")
	startSweeper(t, svc)

	time.Sleep(50 * time.Millisecond)
	if got := stash.scrapeCalls(); len(got) != 0 {
		t.Fatalf("expected no lookups with date_lookup off, got %v", got)
	}
}

func TestDateSweeper_NoBoxesMeansNoScrapes(t *testing.T) {
	loadDateConfig(t, true, false)
	stash := &dateStash{
		scenes: map[string]dateScene{"1": {title: "One", basename: "1.mp4"}},
		boxes:  `[]`,
	}
	svc := NewService(stash)
	if _, err := svc.GetSections(context.Background()); err != nil {
		t.Fatal(err)
	}
	startSweeper(t, svc)

	time.Sleep(50 * time.Millisecond)
	if got := stash.scrapeCalls(); len(got) != 0 {
		t.Fatalf("expected no scrapes without stash-boxes, got %v", got)
	}
	if _, ok := svc.dates().get("1"); ok {
		t.Fatal("nothing may be stored without stash-boxes")
	}
}
