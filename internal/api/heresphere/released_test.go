package heresphere

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
)

// undatedStash answers FindScenes with scene 7: no date, one performer born
// 1990-06-15.
type undatedStash struct{}

func (undatedStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	payload := `{}`
	if req.OpName == "FindScenes" {
		payload = `{"findScenes":{"scenes":[{"id":"7","title":"Seven","created_at":"2024-01-01T00:00:00Z","files":[{"basename":"seven.mp4","duration":100,"path":"/seven.mp4","height":1080,"video_codec":"h264"}],"performers":[{"id":"1","name":"A","alias_list":[],"birthdate":"1990-06-15"}],"tags":[]}]}}`
	}
	return json.Unmarshal([]byte(payload), resp.Data)
}

func undatedSceneWithStoredDate(t *testing.T) *library.VideoData {
	t.Helper()
	loadDefaultRules(t)
	path := filepath.Join(config.Application().ConfigPath, "dates.json")
	if err := os.WriteFile(path, []byte(`{"7":{"date":"2015-01-01","source":"stashdb","checked":"2026-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	vd, err := library.NewService(undatedStash{}).GetScene(context.Background(), "7", false)
	if err != nil {
		t.Fatal(err)
	}
	return vd
}

func TestPerformerFacets_UseLookedUpDate(t *testing.T) {
	vd := undatedSceneWithStoredDate(t)
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

	if got := names(performerFacets(vd, now)); !slices.Equal(got, []string{"Age:24"}) {
		t.Fatalf("expected the age at the looked-up date, got %v", got)
	}
}

func TestGetFields_ReleasedTag(t *testing.T) {
	vd := undatedSceneWithStoredDate(t)
	if got := names(getFields(vd)); !slices.Contains(got, "Released:2015-01-01") {
		t.Fatalf("expected Released from the looked-up date, got %v", got)
	}

	dated := &library.VideoData{SceneParts: &gql.SceneParts{Id: "1", Date: util.Ptr("2020-01-02"),
		Files: []*gql.ScenePartsFilesVideoFile{{Basename: "one.mp4", Duration: 100, Height: 1080}}}}
	if got := names(getFields(dated)); !slices.Contains(got, "Released:2020-01-02") {
		t.Fatalf("expected Released from Stash's date, got %v", got)
	}

	undated := &library.VideoData{SceneParts: &gql.SceneParts{Id: "2",
		Files: []*gql.ScenePartsFilesVideoFile{{Basename: "two.mp4", Duration: 100, Height: 1080}}}}
	for _, name := range names(getFields(undated)) {
		if strings.HasPrefix(name, "Released:") {
			t.Fatalf("expected no Released tag without a date, got %v", name)
		}
	}
}
