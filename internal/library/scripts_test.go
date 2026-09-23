package library

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

// scriptStash answers FindScenes with one scene whose file lives at path.
type scriptStash struct{ path string }

func (f *scriptStash) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	if req.OpName != "FindScenes" {
		return json.Unmarshal([]byte(`{}`), resp.Data)
	}
	scene := map[string]any{
		"id": "7", "title": "Seven", "created_at": "2024-01-01T00:00:00Z", "tags": []any{},
		"files": []map[string]any{{"basename": filepath.Base(f.path), "duration": 100, "path": f.path, "height": 1080, "video_codec": "h264"}},
	}
	b, _ := json.Marshal(map[string]any{"findScenes": map[string]any{"scenes": []any{scene}}})
	return json.Unmarshal(b, resp.Data)
}

func touch(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func labels(vs []ScriptVariant) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Label
	}
	return out
}

func TestScanSiblings_LabelsAndOrder(t *testing.T) {
	dir := t.TempDir()
	stem := "Studio - 2024-01-01 - Title [VR]"
	touch(t, filepath.Join(dir, stem+".mp4"), "video")
	touch(t, filepath.Join(dir, stem+".funscript"), "{}")
	touch(t, filepath.Join(dir, stem+".ai.funscript"), "{}")
	touch(t, filepath.Join(dir, stem+"_v2.funscript"), "{}")
	touch(t, filepath.Join(dir, stem+" alternate 1.funscript"), "{}")
	touch(t, filepath.Join(dir, stem+".roll.funscript"), "{}") // axis, excluded
	touch(t, filepath.Join(dir, stem+"0.funscript"), "{}")     // another video's stem
	touch(t, filepath.Join(dir, stem+"-.funscript"), "{}")     // empty suffix
	touch(t, filepath.Join(dir, "Other.funscript"), "{}")      // unrelated

	got, err := scanSiblings(dir, stem)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Standard", "AI", "Alternate 1", "V2"}
	if g := labels(got); len(g) != len(want) || g[0] != want[0] || g[1] != want[1] || g[2] != want[2] || g[3] != want[3] {
		t.Fatalf("labels = %v, want %v", g, want)
	}
	if got[0].Path != filepath.Join(dir, stem+".funscript") {
		t.Fatalf("Standard path = %q", got[0].Path)
	}
}

func TestScanSiblings_NoStandardStillListsVariants(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "clip.ai.funscript"), "{}")

	got, err := scanSiblings(dir, "clip")
	if err != nil {
		t.Fatal(err)
	}
	if g := labels(got); len(g) != 1 || g[0] != "AI" {
		t.Fatalf("labels = %v", g)
	}
}

func TestScanSiblings_MissingDirIsAnError(t *testing.T) {
	if _, err := scanSiblings(filepath.Join(t.TempDir(), "nope"), "clip"); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestLabelForSuffix(t *testing.T) {
	cases := []struct {
		rest, label string
		ok          bool
	}{
		{".ai", "AI", true}, {".AI", "AI", true}, {"_v2", "V2", true}, {" alternate 1", "Alternate 1", true},
		{"-cut", "Cut", true}, {".roll", "", false}, {".R1", "", false}, {"0", "", false}, {"-", "", false}, {"", "", false},
	}
	for _, c := range cases {
		label, ok := labelForSuffix(c.rest)
		if label != c.label || ok != c.ok {
			t.Errorf("labelForSuffix(%q) = %q,%v want %q,%v", c.rest, label, ok, c.label, c.ok)
		}
	}
}

func TestScriptVariants_CachedUntilReset(t *testing.T) {
	loadConfig(t, nil)
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	touch(t, video, "video")
	touch(t, filepath.Join(dir, "clip.funscript"), "{}")
	s := NewService(&scriptStash{path: video})

	first := s.ScriptVariants(context.Background(), "7")
	touch(t, filepath.Join(dir, "clip.ai.funscript"), "{}")
	second := s.ScriptVariants(context.Background(), "7")
	s.ResetCaches()
	third := s.ScriptVariants(context.Background(), "7")

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected the cached single variant twice, got %v then %v", labels(first), labels(second))
	}
	if g := labels(third); len(g) != 2 || g[1] != "AI" {
		t.Fatalf("expected reset to pick up the new file, got %v", g)
	}
}

func TestScriptVariants_UnknownSceneIsEmpty(t *testing.T) {
	loadConfig(t, nil)
	s := NewService(&scriptStash{path: filepath.Join(t.TempDir(), "clip.mp4")})

	if got := s.ScriptVariants(context.Background(), "999"); len(got) != 0 {
		t.Fatalf("expected no variants, got %v", labels(got))
	}
}
