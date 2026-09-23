package library

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"stash-vr/internal/config"
)

func writeIndex(t *testing.T, dbPath string, rows [][3]string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE script_index (id INTEGER PRIMARY KEY, filename text, metadata text, scene_id text, md5 text)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO script_index (filename, metadata, scene_id, md5) VALUES (?, ?, ?, '')`, r[0], r[1], r[2]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndexScripts_ReadsRowsForScene(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")
	writeIndex(t, dbPath, [][3]string{
		{"/a/one.funscript", `{"creator": "Vurlix"}`, "7"},
		{"/a/two.funscript", `not json`, "7"},
		{"/a/three.funscript", `{"creator": "None"}`, "7"},
		{"/a/other.funscript", `{}`, "8"},
	})

	rows, err := indexScripts(context.Background(), dbPath, "7")
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 3 || rows[0].Path != "/a/one.funscript" || rows[0].Creator != "Vurlix" || rows[1].Creator != "" || rows[2].Creator != "" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestIndexScripts_MissingFileIsAnError(t *testing.T) {
	if _, err := indexScripts(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite"), "7"); err == nil {
		t.Fatal("expected an error for a missing index")
	}
}

func TestMergeAlternates_DedupesByContentAndSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	std := filepath.Join(dir, "clip.funscript")
	copyOfStd := filepath.Join(dir, "elsewhere", "clip.funscript")
	distinct := filepath.Join(dir, "elsewhere", "clip-2.funscript")
	touch(t, std, `{"actions":[1]}`)
	if err := os.MkdirAll(filepath.Dir(copyOfStd), 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, copyOfStd, `{"actions":[1]}`)
	touch(t, distinct, `{"actions":[2]}`)

	got := mergeAlternates(
		[]ScriptVariant{{Label: "Standard", Path: std}},
		[]indexRow{
			{Path: copyOfStd, Creator: "Vurlix"},
			{Path: filepath.Join(dir, "gone.funscript")},
			{Path: distinct, Creator: "Goat"},
		},
	)

	if g := labels(got); len(g) != 2 || g[0] != "Standard" || g[1] != "Alternate 1 (Goat)" {
		t.Fatalf("labels = %v", g)
	}
}

func TestScriptVariants_UsesIndexWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	touch(t, video, "video")
	touch(t, filepath.Join(dir, "clip.funscript"), `{"actions":[1]}`)
	alt := filepath.Join(dir, "alt.funscript")
	touch(t, alt, `{"actions":[2]}`)
	dbPath := filepath.Join(dir, "index.sqlite")
	writeIndex(t, dbPath, [][3]string{{alt, `{}`, "7"}})

	loadConfig(t, nil)
	cfg := config.Application()
	cfg.FunscriptIndexPath = dbPath
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	s := NewService(&scriptStash{path: video})

	if g := labels(s.ScriptVariants(context.Background(), "7")); len(g) != 2 || g[1] != "Alternate 1" {
		t.Fatalf("labels = %v", g)
	}
}

func TestScriptVariants_UnreadableIndexKeepsSiblings(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	touch(t, video, "video")
	touch(t, filepath.Join(dir, "clip.funscript"), `{"actions":[1]}`)

	loadConfig(t, nil)
	cfg := config.Application()
	cfg.FunscriptIndexPath = filepath.Join(dir, "missing.sqlite")
	if _, err := config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	s := NewService(&scriptStash{path: video})

	if g := labels(s.ScriptVariants(context.Background(), "7")); len(g) != 1 || g[0] != "Standard" {
		t.Fatalf("expected the sibling scan to survive a missing index, got %v", g)
	}
	if !s.indexWarned.Load() {
		t.Fatal("expected the missing index to be recorded as warned")
	}
}
