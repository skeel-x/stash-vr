package web

import (
	"errors"
	"net/http"
	"strings"

	"stash-vr/internal/api/internal"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

// maxInspectScenes caps the scenes one inspector search returns.
const maxInspectScenes = 5

// inspectedScene explains how the video rules treat one scene.
type inspectedScene struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Stash   string        `json:"stash"`
	Matched []matchedRule `json:"matched"`
	Format  formatView    `json:"format"`
	Profile profileSource `json:"profile"`
}

// matchedRule is a rule that applies to the scene, by its position in the
// rules table.
type matchedRule struct {
	Index int    `json:"index"`
	Tag   string `json:"tag"`
}

// formatView is a resolved library.Format with unset fields left out.
type formatView struct {
	Projection      string             `json:"projection,omitempty"`
	Stereo          string             `json:"stereo,omitempty"`
	Lens            string             `json:"lens,omitempty"`
	Fov             float32            `json:"fov,omitempty"`
	Passthrough     bool               `json:"passthrough,omitempty"`
	EyeSwap         bool               `json:"eye_swap,omitempty"`
	ForceMono       bool               `json:"force_mono,omitempty"`
	ProfileScene    string             `json:"profile_scene,omitempty"`
	Background      string             `json:"background,omitempty"`
	BackgroundColor string             `json:"background_color,omitempty"`
	Mask            string             `json:"mask,omitempty"`
	Geometry        map[string]float64 `json:"geometry,omitempty"`
	Generated       bool               `json:"generated,omitempty"`
}

// profileSource is where HereSphere gets the scene's profile: Source is a
// library.Profile* value, Scene the id it is served under and Link its
// address, both empty for none.
type profileSource struct {
	Source string `json:"source"`
	Scene  string `json:"scene,omitempty"`
	Link   string `json:"link,omitempty"`
}

func viewFormat(f *library.Format) formatView {
	v := formatView{
		Projection: f.Projection, Stereo: f.Stereo, Lens: f.Lens, Fov: f.Fov,
		Passthrough: f.Passthrough, EyeSwap: f.EyeSwap, ForceMono: f.ForceMono, ProfileScene: f.ProfileScene,
		Background: f.Background, BackgroundColor: f.BackgroundColor, Mask: f.Mask, Generated: f.Generated,
	}
	for _, g := range []struct {
		key   string
		value *float64
	}{
		{"position_x", f.PositionX}, {"position_y", f.PositionY}, {"position_z", f.PositionZ},
		{"pitch", f.Pitch}, {"yaw", f.Yaw}, {"roll", f.Roll},
		{"zoom_x", f.ZoomX}, {"zoom_y", f.ZoomY}, {"pan_x", f.PanX}, {"pan_y", f.PanY},
		{"origin_x", f.OriginX}, {"origin_y", f.OriginY}, {"origin_z", f.OriginZ},
	} {
		if g.value != nil {
			if v.Geometry == nil {
				v.Geometry = map[string]float64{}
			}
			v.Geometry[g.key] = *g.value
		}
	}
	return v
}

// inspectScene resolves the rules for vd the way the players do.
func (h *apiHandler) inspectScene(vd *library.VideoData, rules []config.VideoRule, baseUrl, stashUrl string) inspectedScene {
	id := vd.Id()
	out := inspectedScene{ID: id, Title: vd.Title(), Stash: StashSceneUrl(stashUrl, id), Matched: []matchedRule{}}
	for _, i := range library.MatchingRules(rules, vd.SceneParts.Tags) {
		out.Matched = append(out.Matched, matchedRule{Index: i, Tag: rules[i].Tag})
	}
	f := library.ResolveFormat(rules, vd.SceneParts.Tags)
	out.Format = viewFormat(&f)
	source, scene := library.ProfileSourceFor(id, f, h.lib.HasProfile)
	out.Profile = profileSource{Source: source, Scene: scene}
	if scene != "" {
		out.Profile.Link = baseUrl + "/hsp/scene/" + scene
	}
	return out
}

// inspect explains up to five scenes matching q: the scene with that id,
// then cached scenes whose title contains q. Only the id lookup may query
// Stash; titles are matched against scenes already fetched.
func (h *apiHandler) inspect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(ctx, w, http.StatusBadRequest, "q must name a scene id or part of a title")
		return
	}
	var found []*library.VideoData
	if isDigits(q) {
		vd, err := h.lib.GetScene(ctx, q, false)
		switch {
		case err == nil:
			found = append(found, vd)
		case !errors.Is(err, library.ErrSceneNotFound):
			writeError(ctx, w, http.StatusBadGateway, describeStashError(err))
			return
		}
	}
	for _, vd := range h.lib.SearchCachedScenes(q, maxInspectScenes) {
		if len(found) == maxInspectScenes {
			break
		}
		if len(found) > 0 && found[0].Id() == vd.Id() {
			continue
		}
		found = append(found, vd)
	}
	cfg := config.Application()
	baseUrl := internal.GetBaseUrl(r)
	out := make([]inspectedScene, 0, len(found))
	for _, vd := range found {
		out = append(out, h.inspectScene(vd, cfg.VideoRules, baseUrl, cfg.StashGraphQLUrl))
	}
	writeJson(ctx, w, map[string]any{"scenes": out})
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
