package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

const (
	// coverageTTL is how long a coverage report is reused.
	coverageTTL = 60 * time.Second
	// coverageTimeout bounds the two Stash queries behind one report.
	coverageTimeout = 30 * time.Second
	// maxUntaggedListed caps the untagged VR-shaped scenes listed by name;
	// the count covers all of them.
	maxUntaggedListed = 50
	// vrMinWidth and vrAspectTolerance decide which files look like VR:
	// at least 4K wide and within 3% of 2:1 or 1:1.
	vrMinWidth        = 3840
	vrAspectTolerance = 0.03
)

// coverageTags are the tags the vrQualityTags plugin manages, in the order
// the panel lists them.
var coverageTags = []string{
	"DOME", "SPHERE", "FISHEYE", "FLAT",
	"RF52", "MKX200", "MKX220", "VRCA220",
	"SBS", "TB", "MONO", "RL", "Alpha",
	"VRP: Unresolved", "VRP: Skip",
}

// projectionTags mark a scene's projection; a VR-shaped scene with none of
// them is reported as untagged.
var projectionTags = []string{"DOME", "SPHERE", "FISHEYE", "FLAT", "CUBEMAP", "EAC"}

// coverageReport is what GET /api/ui/coverage returns.
type coverageReport struct {
	Tags      []tagCoverage    `json:"tags"`
	Untagged  untaggedCoverage `json:"untagged"`
	CheckedAt time.Time        `json:"checked_at"`
}

// tagCoverage is one managed tag. Missing is set when Stash has no such
// tag; Link is the Stash scene list for it and is empty then.
type tagCoverage struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Link    string `json:"link,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

// untaggedCoverage counts VR-shaped scenes without a projection tag and
// lists the first of them. Stash cannot filter on aspect ratio, so there
// is no scene list link for the whole set.
type untaggedCoverage struct {
	Count  int             `json:"count"`
	Scenes []untaggedScene `json:"scenes"`
}

type untaggedScene struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Stash  string `json:"stash"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// coverageCache keeps the last report per Stash address for coverageTTL.
// Failures are not kept.
type coverageCache struct {
	mu     sync.Mutex
	now    func() time.Time
	key    string
	at     time.Time
	report *coverageReport
}

func (c *coverageCache) get(key string, load func() (*coverageReport, error)) (*coverageReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if c.report != nil && c.key == key && now().Sub(c.at) < coverageTTL {
		return c.report, nil
	}
	r, err := load()
	if err != nil {
		return nil, err
	}
	c.key, c.at, c.report = key, now(), r
	return r, nil
}

// coverage reports how many scenes carry each tag the vrQualityTags plugin
// manages, and how many VR-shaped scenes have no projection tag.
func (h *apiHandler) coverage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stashUrl := config.Application().StashGraphQLUrl
	report, err := h.coverageCache.get(stashUrl, func() (*coverageReport, error) {
		loadCtx, cancel := context.WithTimeout(ctx, coverageTimeout)
		defer cancel()
		return h.buildCoverage(loadCtx, stashUrl)
	})
	if err != nil {
		writeError(ctx, w, http.StatusBadGateway, describeStashError(err))
		return
	}
	writeJson(ctx, w, report)
}

func (h *apiHandler) buildCoverage(ctx context.Context, stashUrl string) (*coverageReport, error) {
	client := h.lib.Client()
	tagsResp, err := gql.FindTagSceneCounts(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("find tags: %w", err)
	}
	var tags []*gql.FindTagSceneCountsFindTagsFindTagsResultTypeTagsTag
	if tagsResp.FindTags != nil {
		tags = tagsResp.FindTags.Tags
	}
	// Tags match by name case-insensitively, as the video rules do.
	find := func(name string) *gql.FindTagSceneCountsFindTagsFindTagsResultTypeTagsTag {
		for i := range tags {
			if tags[i] != nil && strings.EqualFold(tags[i].Name, name) {
				return tags[i]
			}
		}
		return nil
	}

	report := &coverageReport{Tags: make([]tagCoverage, 0, len(coverageTags)), CheckedAt: time.Now().UTC()}
	for _, name := range coverageTags {
		tag := find(name)
		if tag == nil {
			report.Tags = append(report.Tags, tagCoverage{Name: name, Missing: true})
			continue
		}
		report.Tags = append(report.Tags, tagCoverage{Name: name, Count: tag.Scene_count, Link: StashTagListUrl(stashUrl, tag.Id, tag.Name)})
	}

	filter := &gql.SceneFilterType{Resolution: &gql.ResolutionCriterionInput{
		Modifier: gql.CriterionModifierGreaterThan,
		Value:    gql.ResolutionEnumQuadHd,
	}}
	var exclude []string
	for _, name := range projectionTags {
		if tag := find(name); tag != nil {
			exclude = append(exclude, tag.Id)
		}
	}
	if len(exclude) > 0 {
		depth := 0
		filter.Tags = &gql.HierarchicalMultiCriterionInput{Modifier: gql.CriterionModifierExcludes, Value: exclude, Depth: &depth}
	}
	scenesResp, err := gql.FindSceneDimensions(ctx, client, filter)
	if err != nil {
		return nil, fmt.Errorf("find scenes: %w", err)
	}
	report.Untagged.Scenes = []untaggedScene{}
	if scenesResp.FindScenes != nil {
		for _, s := range scenesResp.FindScenes.Scenes {
			if s == nil || len(s.Files) == 0 || s.Files[0] == nil || !isVrShaped(s.Files[0].Width, s.Files[0].Height) {
				continue
			}
			report.Untagged.Count++
			if len(report.Untagged.Scenes) < maxUntaggedListed {
				report.Untagged.Scenes = append(report.Untagged.Scenes, untaggedScene{
					ID: s.Id, Title: deref(s.Title), Stash: StashSceneUrl(stashUrl, s.Id),
					Width: s.Files[0].Width, Height: s.Files[0].Height,
				})
			}
		}
	}
	return report, nil
}

// isVrShaped reports whether a w x h file looks like VR video: at least 4K
// wide and about 2:1 or 1:1.
func isVrShaped(w, h int) bool {
	if w < vrMinWidth || h <= 0 {
		return false
	}
	ratio := float64(w) / float64(h)
	near := func(target float64) bool {
		d := ratio - target
		return d <= target*vrAspectTolerance && -d <= target*vrAspectTolerance
	}
	return near(2) || near(1)
}

// tagCriterion is a Stash scene list tag criterion, with its fields in the
// order the Stash UI writes them.
type tagCriterion struct {
	Type     string            `json:"type"`
	Modifier string            `json:"modifier"`
	Value    tagCriterionValue `json:"value"`
}

type tagCriterionValue struct {
	Items    []tagCriterionItem `json:"items"`
	Excluded []tagCriterionItem `json:"excluded"`
	Depth    int                `json:"depth"`
}

type tagCriterionItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// StashTagListUrl is the Stash web UI scene list filtered to the tag with
// this id, encoded the way Stash's ListFilterModel encodes a c= criterion:
// braces outside strings become parentheses, then the JSON is encodeURI'd
// and ?#&;=+ are escaped as well.
func StashTagListUrl(graphqlUrl, id, name string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// Encoding a struct of strings and ints cannot fail.
	_ = enc.Encode(tagCriterion{
		Type:     "tags",
		Modifier: "INCLUDES_ALL",
		Value:    tagCriterionValue{Items: []tagCriterionItem{{ID: id, Label: name}}, Excluded: []tagCriterionItem{}},
	})
	s := translateCriterion(strings.TrimSuffix(buf.String(), "\n"), false)
	return stashWebBase(graphqlUrl) + "/scenes?c=" + encodeCriterion(s)
}

// translateCriterion swaps { and } for ( and ) outside JSON strings when
// encoding, and back when decoding, as Stash's translateJSON does.
func translateCriterion(s string, decoding bool) string {
	var b strings.Builder
	inString, escaped := false, false
	for _, c := range s {
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case decoding && c == '(':
			c = '{'
		case decoding && c == ')':
			c = '}'
		case !decoding && c == '{':
			c = '('
		case !decoding && c == '}':
			c = ')'
		}
		b.WriteRune(c)
	}
	return b.String()
}

// encodeCriterion percent-encodes s like JavaScript's encodeURI followed
// by escaping ?#&;=+, which is what Stash writes into the address bar.
func encodeCriterion(s string) string {
	const keep = "-_.!~*'(),/:@$"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
