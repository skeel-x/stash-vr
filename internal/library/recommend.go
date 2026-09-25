package library

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// recScene is one row of the recommendation query.
type recScene = gql.FindRecommendationScenesFindScenesFindScenesResultTypeScenesScene

const (
	recDay = 24 * time.Hour
	// recWindow is how far back the history reaches while at least
	// recMinHistory scenes were played in it; with fewer it covers all time.
	recWindow     = 90 * recDay
	recMinHistory = 10
	// recHalfLife halves a history scene's weight every 30 days since it
	// was last played; scenes never played count as recUndatedAge old.
	recHalfLife   = 30 * recDay
	recUndatedAge = 60 * recDay
	// recFinishedSlack is how much resume position, in seconds, still
	// counts as watched to the end.
	recFinishedSlack = 10.0

	// Feature weights: a shared performer says more than a shared tag or studio.
	recTagWeight       = 1.0
	recPerformerWeight = 2.0
	recStudioWeight    = 1.0
)

// recFormatTags are the format and quality tags the vrQualityTags plugin
// manages. They describe the file rather than the content, so they are not
// taste features. Names are matched case-insensitively; tags starting with
// recFormatTagPrefix are also left out.
var recFormatTags = map[string]struct{}{
	"dome": {}, "sphere": {}, "fisheye": {}, "flat": {}, "rf52": {}, "mkx200": {}, "mkx220": {}, "vrca220": {},
	"sbs": {}, "tb": {}, "mono": {}, "rl": {}, "alpha": {}, "chroma key": {}, "8k": {}, "7k": {}, "6k hbr": {}, "hq": {},
}

const recFormatTagPrefix = "vrp:"

// recVRTags mark a scene as VR by tag.
var recVRTags = map[string]struct{}{"dome": {}, "sphere": {}, "fisheye": {}}

// recFeature is one weighted feature of a scene: "t:<id>", "p:<id>" or "s:<id>".
type recFeature struct {
	key    string
	weight float64
}

// historyScene is a scene the user engaged with and how much it counts.
type historyScene struct {
	scene  *recScene
	weight float64
}

func intVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func floatVal(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func played(s *recScene) bool {
	return intVal(s.Play_count) > 0 || floatVal(s.Resume_time) > 0
}

// liked reports a scene that counts as history without being played.
func liked(s *recScene) bool {
	return intVal(s.O_counter) > 0 || intVal(s.Rating100) >= 80
}

// historyWeight is 1, +1 when watched to the end, +o-count (at most 3),
// +(rating-60)/20 above a rating of 60, halved for every recHalfLife since
// the scene was last played.
func historyWeight(s *recScene, now time.Time) float64 {
	w := 1.0
	if intVal(s.Play_count) > 0 && floatVal(s.Resume_time) < recFinishedSlack {
		w++
	}
	w += float64(min(intVal(s.O_counter), 3))
	if r := intVal(s.Rating100); r > 60 {
		w += float64(r-60) / 20
	}
	age := recUndatedAge
	if s.Last_played_at != nil {
		age = max(now.Sub(*s.Last_played_at), 0)
	}
	return w * math.Exp2(-float64(age)/float64(recHalfLife))
}

// pickHistory returns the scenes played in the last recWindow plus those
// with an o-count or a rating of 80 or more that were last played in it or
// never; with fewer than recMinHistory scenes played in the window, every
// played, o-counted or highly rated scene.
func pickHistory(scenes []*recScene, now time.Time) []historyScene {
	inWindow := func(s *recScene) bool {
		return s.Last_played_at != nil && now.Sub(*s.Last_played_at) <= recWindow
	}
	recent := 0
	for _, s := range scenes {
		if s != nil && played(s) && inWindow(s) {
			recent++
		}
	}
	allTime := recent < recMinHistory
	var out []historyScene
	for _, s := range scenes {
		if s == nil {
			continue
		}
		var take bool
		switch {
		case allTime:
			take = played(s) || liked(s)
		case played(s):
			take = inWindow(s)
		default:
			take = liked(s) && (s.Last_played_at == nil || inWindow(s))
		}
		if take {
			out = append(out, historyScene{scene: s, weight: historyWeight(s, now)})
		}
	}
	return out
}

// isVR reports a scene tagged DOME, SPHERE or FISHEYE, or with a file at
// least 3840 wide in about 2:1 or 1:1.
func isVR(s *recScene) bool {
	for _, t := range s.Tags {
		if t == nil {
			continue
		}
		if _, ok := recVRTags[strings.ToLower(t.Name)]; ok {
			return true
		}
	}
	for _, f := range s.Files {
		if f == nil || f.Width < 3840 || f.Height <= 0 {
			continue
		}
		ratio := float64(f.Width) / float64(f.Height)
		if math.Abs(ratio-2) <= 0.1 || math.Abs(ratio-1) <= 0.1 {
			return true
		}
	}
	return false
}

// excludedTag reports a tag that is not a taste feature: hidden by the
// configured sort name or managed by vrQualityTags.
func excludedTag(t *gql.FindRecommendationScenesFindScenesFindScenesResultTypeScenesSceneTagsTag, excludeSortName string) bool {
	if t.Sort_name != nil && *t.Sort_name == excludeSortName {
		return true
	}
	name := strings.ToLower(t.Name)
	if _, ok := recFormatTags[name]; ok {
		return true
	}
	return strings.HasPrefix(name, recFormatTagPrefix)
}

// features lists a scene's tags, performers and studio, each once.
func features(s *recScene, excludeSortName string) []recFeature {
	var out []recFeature
	seen := map[string]struct{}{}
	add := func(key string, weight float64) {
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, recFeature{key: key, weight: weight})
	}
	for _, t := range s.Tags {
		if t != nil && !excludedTag(t, excludeSortName) {
			add("t:"+t.Id, recTagWeight)
		}
	}
	for _, p := range s.Performers {
		if p != nil {
			add("p:"+p.Id, recPerformerWeight)
		}
	}
	if s.Studio != nil {
		add("s:"+s.Studio.Id, recStudioWeight)
	}
	return out
}

// idf is the inverse document frequency of a feature on df of n scenes,
// never below 0.
func idf(n, df int) float64 {
	return max(math.Log(float64(n)/float64(1+df)), 0)
}

// recommend returns up to size scenes the user has not played yet, ranked
// by how well their tags, performers and studio match what the user
// watched, rated and o-counted recently (see pickHistory and
// historyWeight). The profile is the history's weighted feature sum; a
// candidate scores the sum of profile weight times idf over its features,
// divided by the square root of its feature count. Only scenes of the
// history's format class (VR or not, by weight) are candidates; scenes
// scoring 0 are left out, equal scores keep the query order (newest first).
func recommend(scenes []*recScene, excludeSortName string, now time.Time, size int) []string {
	history := pickHistory(scenes, now)
	if len(history) == 0 {
		return nil
	}

	feats := make(map[*recScene][]recFeature, len(scenes))
	df := map[string]int{}
	n := 0
	for _, s := range scenes {
		if s == nil {
			continue
		}
		n++
		f := features(s, excludeSortName)
		feats[s] = f
		for _, x := range f {
			df[x.key]++
		}
	}

	inHistory := make(map[*recScene]struct{}, len(history))
	profile := map[string]float64{}
	var total, vrWeight float64
	for _, h := range history {
		inHistory[h.scene] = struct{}{}
		total += h.weight
		if isVR(h.scene) {
			vrWeight += h.weight
		}
		for _, x := range feats[h.scene] {
			profile[x.key] += h.weight * x.weight
		}
	}
	wantVR := vrWeight > total/2

	type scored struct {
		id    string
		score float64
	}
	var ranked []scored
	for _, s := range scenes {
		if s == nil || played(s) || len(s.Files) == 0 || isVR(s) != wantVR {
			continue
		}
		if _, ok := inHistory[s]; ok {
			continue
		}
		f := feats[s]
		if len(f) == 0 {
			continue
		}
		var sum float64
		for _, x := range f {
			sum += profile[x.key] * idf(n, df[x.key])
		}
		if sum <= 0 {
			continue
		}
		ranked = append(ranked, scored{id: s.Id, score: sum / math.Sqrt(float64(len(f)))})
	}
	slices.SortStableFunc(ranked, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	if len(ranked) > size {
		ranked = ranked[:size]
	}
	out := make([]string, len(ranked))
	for i, r := range ranked {
		out[i] = r.id
	}
	return out
}

// recCacheTTL bounds how often the whole library is fetched and scored;
// taste moves slower than the 60 s index.
const recCacheTTL = 30 * time.Minute

// recCache is one computed recommendation and what it was computed for.
type recCache struct {
	ids             []string
	at              time.Time
	size            int
	excludeSortName string
}

// recommendedIDs returns the Recommended for you ids, computing them at
// most once per recCacheTTL (or when the size or excluded sort name
// changes). An empty result is cached too; errors are not.
func (libraryService *Service) recommendedIDs(ctx context.Context, size int) ([]string, error) {
	exclude := config.Application().ExcludeSortName
	now := libraryService.now()

	libraryService.muRec.Lock()
	c, gen := libraryService.rec, libraryService.recGen
	libraryService.muRec.Unlock()
	if c != nil && c.size == size && c.excludeSortName == exclude && now.Sub(c.at) < recCacheTTL {
		return slices.Clone(c.ids), nil
	}

	resp, err := gql.FindRecommendationScenes(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("FindRecommendationScenes: %w", err)
	}
	var scenes []*recScene
	if resp.FindScenes != nil {
		scenes = resp.FindScenes.Scenes
	}
	ids := recommend(scenes, exclude, now, size)

	libraryService.muRec.Lock()
	if libraryService.recGen == gen {
		libraryService.rec = &recCache{ids: ids, at: now, size: size, excludeSortName: exclude}
	}
	libraryService.muRec.Unlock()
	return slices.Clone(ids), nil
}
