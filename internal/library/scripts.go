package library

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"stash-vr/internal/config"

	"github.com/rs/zerolog/log"
)

// ScriptVariant is one funscript available for a scene.
type ScriptVariant struct {
	Label string // "Standard", "AI", "V2", "Alternate 1 (Vurlix)"
	Path  string // absolute path on disk
}

const (
	scriptCacheTTL = 5 * time.Minute
	scriptExt      = ".funscript"
	labelStandard  = "Standard"
)

type scriptEntry struct {
	at       time.Time
	variants []ScriptVariant
}

// axisSuffixes name multi-axis device channels; they are companions of the
// main script, not alternatives to it.
var axisSuffixes = map[string]struct{}{
	"roll": {}, "pitch": {}, "twist": {}, "sway": {}, "surge": {},
	"l1": {}, "l2": {}, "r0": {}, "r1": {}, "r2": {},
}

// ScriptVariants lists the funscripts for a scene: Standard first, then
// sibling variants by label. Discovery never fails a request: any problem
// yields an empty list and a debug log. Results are cached per scene for
// scriptCacheTTL and dropped by ResetCaches.
func (libraryService *Service) ScriptVariants(ctx context.Context, id string) []ScriptVariant {
	libraryService.muScripts.Lock()
	if e, ok := libraryService.scriptCache[id]; ok && time.Since(e.at) < scriptCacheTTL {
		libraryService.muScripts.Unlock()
		return e.variants
	}
	libraryService.muScripts.Unlock()

	variants := libraryService.discoverScripts(ctx, id)

	libraryService.muScripts.Lock()
	libraryService.scriptCache[id] = scriptEntry{at: time.Now(), variants: variants}
	libraryService.muScripts.Unlock()
	return variants
}

func (libraryService *Service) discoverScripts(ctx context.Context, id string) []ScriptVariant {
	vd, err := libraryService.GetScene(ctx, id, false)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Str("scene", id).Msg("Script discovery: scene unavailable")
		return []ScriptVariant{}
	}
	dir, stem, ok := scriptStem(vd)
	if !ok {
		return []ScriptVariant{}
	}
	variants, err := scanSiblings(dir, stem)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Str("scene", id).Msg("Script discovery: directory unreadable")
		return []ScriptVariant{}
	}
	if dbPath := config.Application().FunscriptIndexPath; dbPath != "" {
		rows, err := indexScripts(ctx, dbPath, id)
		if err != nil {
			if libraryService.indexWarned.CompareAndSwap(false, true) {
				log.Ctx(ctx).Warn().Err(err).Str("path", dbPath).Msg("Funscript index unavailable; alternates disabled until it can be read")
			}
			return variants
		}
		libraryService.indexWarned.Store(false)
		variants = mergeAlternates(variants, rows)
	}
	return variants
}

// scriptStem returns the directory and file stem of the scene's first file.
func scriptStem(vd *VideoData) (dir, stem string, ok bool) {
	if vd == nil || vd.SceneParts == nil || len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil || vd.SceneParts.Files[0].Path == "" {
		return "", "", false
	}
	p := vd.SceneParts.Files[0].Path
	base := filepath.Base(p)
	return filepath.Dir(p), strings.TrimSuffix(base, filepath.Ext(base)), true
}

// scanSiblings lists <stem>*.funscript in dir: Standard first, then the
// variants sorted by label. os.ReadDir is used instead of filepath.Glob
// because stems contain glob metacharacters such as "[VR]".
func scanSiblings(dir, stem string) ([]ScriptVariant, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var standard *ScriptVariant
	variants := []ScriptVariant{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, stem) || !strings.HasSuffix(name, scriptExt) {
			continue
		}
		rest := strings.TrimSuffix(strings.TrimPrefix(name, stem), scriptExt)
		path := filepath.Join(dir, name)
		if rest == "" {
			standard = &ScriptVariant{Label: labelStandard, Path: path}
			continue
		}
		if label, ok := labelForSuffix(rest); ok {
			variants = append(variants, ScriptVariant{Label: label, Path: path})
		}
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i].Label < variants[j].Label })
	if standard != nil {
		variants = append([]ScriptVariant{*standard}, variants...)
	}
	return variants, nil
}

// labelForSuffix turns the text between a stem and ".funscript" into a
// label. rest must start with a separator, trim to something non-empty and
// not name a device axis.
func labelForSuffix(rest string) (string, bool) {
	if rest == "" || !strings.ContainsRune("._- ", rune(rest[0])) {
		return "", false
	}
	suffix := strings.Trim(rest, "._- ")
	if suffix == "" {
		return "", false
	}
	lower := strings.ToLower(suffix)
	if _, axis := axisSuffixes[lower]; axis {
		return "", false
	}
	if lower == "ai" {
		return "AI", true
	}
	r, size := utf8.DecodeRuneInString(suffix)
	return string(unicode.ToUpper(r)) + suffix[size:], true
}
