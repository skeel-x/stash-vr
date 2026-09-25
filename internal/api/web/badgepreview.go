package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/stash/gql"
)

const (
	// previewSceneTTL is how long the default preview scene is reused.
	previewSceneTTL = 10 * time.Minute
	// previewSceneTimeout bounds the Stash queries picking it.
	previewSceneTimeout = 15 * time.Second
	// alphaTag marks alpha-packed passthrough scenes; a default preview
	// scene with it shows the AR badge too.
	alphaTag = "Alpha"
)

// errNoPreviewScene means Stash has no scene with a cover to preview.
var errNoPreviewScene = errors.New("no scene with a cover to preview")

// previewSceneCache keeps the default preview scene per Stash address
// for previewSceneTTL. Failures are not kept.
type previewSceneCache struct {
	mu  sync.Mutex
	now func() time.Time
	key string
	at  time.Time
	id  string
}

func (c *previewSceneCache) get(key string, load func() (string, error)) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if c.id != "" && c.key == key && now().Sub(c.at) < previewSceneTTL {
		return c.id, nil
	}
	id, err := load()
	if err != nil {
		return "", err
	}
	c.key, c.at, c.id = key, now(), id
	return id, nil
}

// badgePreview renders a scene's cover with the badges switched on in the
// query (quality, format, passthrough) instead of the saved settings, so
// the Setup page can show them before they are saved. scene picks the
// scene; without it a scene with a tier tag, and an Alpha tag if there is
// one, is chosen. The response is never cached and the cover is not kept
// in the rendered cover cache. The scene id and its percent-encoded title
// come back in X-Scene-Id and X-Scene-Title.
func (h *apiHandler) badgePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	on, err := previewFlags(q)
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	id := strings.TrimSpace(q.Get("scene"))
	switch {
	case id == "":
		stashUrl := config.Application().StashGraphQLUrl
		id, err = h.previewScene.get(stashUrl, func() (string, error) {
			loadCtx, cancel := context.WithTimeout(ctx, previewSceneTimeout)
			defer cancel()
			return findPreviewScene(loadCtx, h.lib)
		})
		if errors.Is(err, errNoPreviewScene) {
			writeError(ctx, w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeError(ctx, w, http.StatusBadGateway, describeStashError(err))
			return
		}
	case !isDigits(id):
		writeError(ctx, w, http.StatusBadRequest, "scene must be a scene id")
		return
	}

	vd, err := h.lib.GetScene(ctx, id, false)
	if errors.Is(err, library.ErrSceneNotFound) {
		writeError(ctx, w, http.StatusNotFound, "no scene with id "+id)
		return
	}
	if err != nil {
		writeError(ctx, w, http.StatusBadGateway, describeStashError(err))
		return
	}
	p := vd.SceneParts.Paths
	if p == nil || p.Screenshot == nil || *p.Screenshot == "" {
		writeError(ctx, w, http.StatusNotFound, "scene "+id+" has no cover")
		return
	}

	badges := coverbadge.ForScene(vd, on, config.Application().VideoRules)
	body, err := heatmap.RenderPreview(ctx, stash.ApiKeyed(*p.Screenshot), heatmap.SceneHeatmapURL(vd), badges)
	if errors.Is(err, heatmap.ErrImageNotFound()) {
		writeError(ctx, w, http.StatusNotFound, "scene "+id+" has no cover")
		return
	}
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("badge preview")
		writeError(ctx, w, http.StatusBadGateway, "could not render the cover")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Scene-Id", vd.Id())
	w.Header().Set("X-Scene-Title", url.PathEscape(vd.Title()))
	if _, err := w.Write(body); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("badge preview: write")
	}
}

// previewFlags reads the quality, format and passthrough switches; a
// missing one is off.
func previewFlags(q url.Values) (config.CoverBadges, error) {
	var on config.CoverBadges
	for _, f := range []struct {
		name string
		dst  *bool
	}{{"quality", &on.Quality}, {"format", &on.Format}, {"passthrough", &on.Passthrough}} {
		v := q.Get(f.name)
		if v == "" {
			continue
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return on, fmt.Errorf("%s must be 0 or 1", f.name)
		}
		*f.dst = b
	}
	return on, nil
}

// findPreviewScene picks a scene that shows every badge kind: one with a
// tier tag and, when Stash has the tag, Alpha; else one with a tier tag;
// else any scene with a cover. Each is a one-scene query.
func findPreviewScene(ctx context.Context, lib *library.Service) (string, error) {
	client := lib.Client()
	tagIDs := func(names ...string) ([]string, error) {
		var ids []string
		for _, name := range names {
			resp, err := gql.FindTagByName(ctx, client, name)
			if err != nil {
				return nil, fmt.Errorf("find tag %s: %w", name, err)
			}
			if resp.FindTags == nil {
				continue
			}
			for _, t := range resp.FindTags.Tags {
				if t != nil {
					ids = append(ids, t.Id)
				}
			}
		}
		return ids, nil
	}
	tiers, err := tagIDs(coverbadge.TierTags()...)
	if err != nil {
		return "", err
	}
	alpha, err := tagIDs(alphaTag)
	if err != nil {
		return "", err
	}

	missing := "cover"
	withCover := &gql.SceneFilterType{Is_missing: &missing}
	var tries []*gql.SceneFilterType
	if len(tiers) > 0 {
		if len(alpha) > 0 {
			// Stash allows one of AND, OR and NOT per level, so the cover
			// condition nests inside the Alpha one.
			tries = append(tries, &gql.SceneFilterType{Tags: includesAny(tiers), AND: &gql.SceneFilterType{Tags: includesAny(alpha), NOT: withCover}})
		}
		tries = append(tries, &gql.SceneFilterType{Tags: includesAny(tiers), NOT: withCover})
	}
	tries = append(tries, &gql.SceneFilterType{NOT: withCover})

	one, sort := 1, "random"
	opts := &gql.FindFilterType{Per_page: &one, Sort: &sort}
	for _, f := range tries {
		resp, err := gql.FindSceneIdsByFilter(ctx, client, f, opts)
		if err != nil {
			return "", fmt.Errorf("find scenes: %w", err)
		}
		if resp.FindScenes != nil {
			for _, s := range resp.FindScenes.Scenes {
				if s != nil && s.Id != "" {
					return s.Id, nil
				}
			}
		}
	}
	return "", errNoPreviewScene
}

// includesAny matches scenes carrying any of the tags, without child tags.
func includesAny(ids []string) *gql.HierarchicalMultiCriterionInput {
	depth := 0
	return &gql.HierarchicalMultiCriterionInput{Modifier: gql.CriterionModifierIncludes, Value: ids, Depth: &depth}
}
