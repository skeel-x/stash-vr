package library

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// dsnPath escapes the characters that would end the path in a SQLite URI.
var dsnPath = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")

// indexRow is one script the timestampTrade plugin index lists for a scene.
type indexRow struct {
	Path    string
	Creator string
}

// indexScripts reads the plugin's script_index rows for a scene, in
// filename order. The database is opened read-only.
func indexScripts(ctx context.Context, dbPath, sceneId string) ([]indexRow, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	// Read-only, and wait briefly if the plugin happens to be writing.
	db, err := sql.Open("sqlite", "file:"+dsnPath.Replace(dbPath)+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT filename, metadata FROM script_index WHERE scene_id = ? ORDER BY filename`, sceneId)
	if err != nil {
		return nil, fmt.Errorf("query index: %w", err)
	}
	defer rows.Close()

	out := []indexRow{}
	for rows.Next() {
		var filename string
		var metadata sql.NullString
		if err := rows.Scan(&filename, &metadata); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		out = append(out, indexRow{Path: filename, Creator: creatorFrom(metadata.String)})
	}
	return out, rows.Err()
}

// creatorFrom extracts a usable creator name from the row's metadata JSON.
func creatorFrom(metadata string) string {
	var m map[string]any
	if json.Unmarshal([]byte(metadata), &m) != nil {
		return ""
	}
	if c, ok := m["creator"].(string); ok && c != "" && c != "None" {
		return c
	}
	return ""
}

// mergeAlternates appends index rows to existing as "Alternate N" entries,
// skipping files that are missing or byte-identical to a script already
// in the list.
func mergeAlternates(existing []ScriptVariant, rows []indexRow) []ScriptVariant {
	if len(rows) == 0 {
		return existing
	}
	seen := map[string]struct{}{}
	for _, v := range existing {
		if sum, err := fileMD5(v.Path); err == nil {
			seen[sum] = struct{}{}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	out := append([]ScriptVariant{}, existing...)
	n := 0
	for _, r := range rows {
		sum, err := fileMD5(r.Path)
		if err != nil {
			continue
		}
		if _, dup := seen[sum]; dup {
			continue
		}
		seen[sum] = struct{}{}
		n++
		label := fmt.Sprintf("Alternate %d", n)
		if r.Creator != "" {
			label += " (" + r.Creator + ")"
		}
		out = append(out, ScriptVariant{Label: label, Path: r.Path})
	}
	return out
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
