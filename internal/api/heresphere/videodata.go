package heresphere

import (
	"context"
	"fmt"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
	"time"

	"github.com/rs/zerolog/log"
)

type videoDataDto struct {
	Access int `json:"access"`

	Title               string          `json:"title"`
	Description         string          `json:"description,omitempty"`
	ThumbnailImage      *string         `json:"thumbnailImage,omitempty"`
	ThumbnailVideo      *string         `json:"thumbnailVideo,omitempty"`
	DateReleased        *string         `json:"dateReleased,omitempty"`
	DateAdded           string          `json:"dateAdded,omitempty"`
	Duration            float64         `json:"duration,omitempty"`
	Rating              *float32        `json:"rating,omitempty"`
	Favorites           *int            `json:"favorites,omitempty"`
	Comments            *int            `json:"comments,omitempty"`
	IsFavorite          *bool           `json:"isFavorite,omitempty"`
	Projection          string          `json:"projection,omitempty"`
	Stereo              string          `json:"stereo,omitempty"`
	Fov                 float32         `json:"fov,omitempty"`
	Lens                string          `json:"lens,omitempty"`
	Hsp                 *string         `json:"hsp,omitempty"`
	AlphaPackedSettings *alphaPackedDto `json:"alphaPackedSettings,omitempty"`
	EventServer         *string         `json:"eventServer,omitempty"`
	Scripts             []scriptDto     `json:"scripts,omitempty"`
	Tags                []tagDto        `json:"tags,omitempty"`
	Media               []mediaDto      `json:"media,omitempty"`
	Subtitles           []subtitleDto   `json:"subtitles,omitempty"`

	WriteFavorite *bool `json:"writeFavorite,omitempty"`
	WriteRating   *bool `json:"writeRating,omitempty"`
	WriteTags     *bool `json:"writeTags,omitempty"`
	WriteHSP      *bool `json:"writeHSP,omitempty"`
}

type alphaPackedDto struct {
	DefaultSettings bool `json:"defaultSettings"`
}

type mediaDto struct {
	Name    string      `json:"name,omitempty"`
	Sources []sourceDto `json:"sources,omitempty"`
}

type sourceDto struct {
	Resolution int    `json:"resolution,omitempty"`
	Url        string `json:"url,omitempty"`
}

type scriptDto struct {
	Name string `json:"name,omitempty"`
	Url  string `json:"url,omitempty"`
}

type subtitleDto struct {
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`
	Url      string `json:"url,omitempty"`
}

// profileLookup answers whether a HereSphere profile is stored for a
// scene and which scene's profile it learns from its studio and lens.
type profileLookup interface {
	HasProfile(id string) bool
	StudioProfile(ctx context.Context, vd *library.VideoData, f *library.Format) string
}

func buildVideoData(ctx context.Context, vd *library.VideoData, baseUrl string, variants []library.ScriptVariant, profiles profileLookup) (*videoDataDto, error) {
	videoId := vd.Id()
	if len(vd.SceneParts.Files) == 0 {
		return nil, fmt.Errorf("scene %s has no files", videoId)
	}

	dto := videoDataDto{
		Access:        1,
		Title:         vd.Title(),
		Description:   vd.Description(),
		DateAdded:     vd.SceneParts.Created_at.Format(time.DateOnly),
		Duration:      vd.SceneParts.Files[0].Duration * 1000,
		WriteFavorite: util.Ptr(true),
		WriteRating:   util.Ptr(true),
		WriteTags:     util.Ptr(true),
		WriteHSP:      util.Ptr(true),
		EventServer:   util.Ptr(getEventsUrl(baseUrl, videoId)),
	}

	paths := vd.SceneParts.Paths
	if paths != nil {
		if paths.Screenshot != nil && *paths.Screenshot != "" {
			dto.ThumbnailImage = util.Ptr(heatmap.GetCoverUrl(baseUrl, videoId))
		}

		if paths.Preview != nil {
			dto.ThumbnailVideo = util.Ptr(stash.ApiKeyed(*paths.Preview))
		}
	}

	if d := vd.ReleaseDate(); d != "" {
		dto.DateReleased = util.Ptr(util.NormalizeDate(d))
	}

	if vd.SceneParts.Rating100 != nil {
		dto.Rating = util.Ptr(float32(*vd.SceneParts.Rating100) / 20)
	}

	if vd.SceneParts.Play_count != nil {
		dto.Comments = util.Ptr(*vd.SceneParts.Play_count)
	}

	if vd.SceneParts.O_counter != nil {
		dto.Favorites = vd.SceneParts.O_counter
	}

	if isFavorite(vd) {
		dto.IsFavorite = util.Ptr(true)
	}

	setMediaSources(vd, &dto)

	cfg := config.Application()
	f, _ := library.SceneFormat(cfg.VideoRules, vd, cfg.CorrectVerticalStereo)
	setFormat(vd, &dto, f)
	if link := profileLink(ctx, baseUrl, vd, f, profiles); link != "" {
		dto.Hsp = util.Ptr(link)
	}

	setScripts(vd, &dto, baseUrl, variants)

	setSubtitles(vd, &dto)

	dto.Tags = getTags(vd)

	// The preview and media URLs carry the Stash API key; it must never
	// reach the log or the Log page.
	log.Ctx(ctx).Debug().
		Str("thumbImage", derefOr(dto.ThumbnailImage)).
		Str("thumbVideo", stash.Redacted(derefOr(dto.ThumbnailVideo))).
		Str("codec", vd.SceneParts.Files[0].Video_codec).
		Interface("media", redactedMedia(dto.Media)).Send()

	return &dto, nil
}

// redactedMedia copies media with the Stash API key stripped from every
// source URL, for the debug log.
func redactedMedia(media []mediaDto) []mediaDto {
	out := make([]mediaDto, len(media))
	for i := range media {
		out[i] = mediaDto{Name: media[i].Name, Sources: make([]sourceDto, len(media[i].Sources))}
		for j, s := range media[i].Sources {
			out[i].Sources[j] = sourceDto{Resolution: s.Resolution, Url: stash.Redacted(s.Url)}
		}
	}
	return out
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func setSubtitles(vd *library.VideoData, dto *videoDataDto) {
	if vd.SceneParts.Captions == nil {
		return
	}
	for _, c := range vd.SceneParts.Captions {
		dto.Subtitles = append(dto.Subtitles, subtitleDto{
			Name:     fmt.Sprintf("%s.%s", c.Language_code, c.Caption_type),
			Language: c.Language_code,
			Url:      stash.ApiKeyed(fmt.Sprintf("%s?lang=%s&type=%s", *vd.SceneParts.Paths.Caption, c.Language_code, c.Caption_type)),
		})
	}
}

func isFavorite(vd *library.VideoData) bool {
	for _, t := range vd.SceneParts.Tags {
		if t.Name == config.Application().FavoriteTag {
			return true
		}
	}
	return false
}

// setScripts lists every funscript for the scene. Standard keeps the URL
// Stash serves (API-keyed) when Stash knows it; every other variant is
// served by stash-vr from disk by its index in the variant list, with the
// variant's key so a changed list cannot serve the wrong script.
func setScripts(vd *library.VideoData, dto *videoDataDto, baseUrl string, variants []library.ScriptVariant) {
	stashUrl := ""
	if vd.SceneParts.Paths != nil && vd.SceneParts.Paths.Funscript != nil && *vd.SceneParts.Paths.Funscript != "" {
		stashUrl = stash.ApiKeyed(*vd.SceneParts.Paths.Funscript)
	}
	if len(variants) == 0 {
		if vd.SceneParts.Interactive && stashUrl != "" {
			dto.Scripts = append(dto.Scripts, scriptDto{Name: "Standard", Url: stashUrl})
		}
		return
	}
	hasStandard := false
	for i, v := range variants {
		u := fmt.Sprintf("%s/funscript/%s/%d?k=%s", baseUrl, vd.Id(), i, v.Key())
		if v.Label == "Standard" {
			hasStandard = true
			if stashUrl != "" {
				u = stashUrl
			}
		}
		dto.Scripts = append(dto.Scripts, scriptDto{Name: v.Label, Url: u})
	}
	// Stash still knows a script the scan did not find (renamed or moved
	// since Stash last scanned): keep it in front rather than losing it.
	if !hasStandard && stashUrl != "" {
		dto.Scripts = append([]scriptDto{{Name: "Standard", Url: stashUrl}}, dto.Scripts...)
	}
}

// setFormat writes the resolved rule format into the HereSphere fields.
func setFormat(vd *library.VideoData, dto *videoDataDto, f library.Format) {
	dto.Projection = f.Projection
	dto.Stereo = f.Stereo
	dto.Lens = f.Lens
	dto.Fov = f.Fov
	// HereSphere only applies a fisheye scene's fov when the response also
	// names a lens; without one it silently falls back to 180. Rules like
	// RF52 set a fov (190) but no lens, so default to the linear lens to
	// honour the fov. Lensed rules (MKX200 etc.) keep their own lens.
	if dto.Projection == "fisheye" && dto.Lens == "" && dto.Fov != 0 {
		dto.Lens = "Linear"
	}
	if f.Passthrough {
		dto.AlphaPackedSettings = &alphaPackedDto{DefaultSettings: true}
	}
}

// profileLink links the HereSphere profile for the scene when it has one
// (see library.ProfileSourceFor). Every profile is served under the
// scene's own id: the hsp handler resolves the source again and serves a
// profile learned from another scene rebased on this one.
func profileLink(ctx context.Context, baseUrl string, vd *library.VideoData, f library.Format, profiles profileLookup) string {
	if profiles == nil {
		return ""
	}
	learned := func() string { return profiles.StudioProfile(ctx, vd, &f) }
	if _, scene := library.ProfileSourceFor(vd.Id(), f, profiles.HasProfile, learned); scene != "" {
		return baseUrl + "/hsp/scene/" + vd.Id()
	}
	return ""
}

func setMediaSources(vd *library.VideoData, dto *videoDataDto) {
	streams := []stash.Stream{stash.GetDirectStream(vd.SceneParts), stash.GetTranscodingStream(vd.SceneParts)}
	for _, stream := range streams {
		e := mediaDto{
			Name: stream.Name,
		}
		for _, s := range stream.Sources {
			vs := sourceDto{
				Resolution: s.Resolution,
				Url:        s.Url,
			}
			e.Sources = append(e.Sources, vs)
		}
		dto.Media = append(dto.Media, e)
	}
}
