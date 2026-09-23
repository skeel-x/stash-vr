package deovr

import (
	"fmt"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
	"strings"
)

type videoDataDto struct {
	Authorized     string  `json:"authorized"`
	FullAccess     bool    `json:"fullAccess"`
	Title          string  `json:"title"`
	Id             string  `json:"id"`
	VideoLength    int     `json:"videoLength"`
	Is3d           bool    `json:"is3d"`
	ScreenType     string  `json:"screenType"`
	StereoMode     string  `json:"stereoMode"`
	SkipIntro      int     `json:"skipIntro"`
	VideoThumbnail *string `json:"videoThumbnail,omitempty"`
	VideoPreview   *string `json:"videoPreview,omitempty"`
	ThumbnailUrl   *string `json:"thumbnailUrl"`

	TimeStamps []timeStampDto `json:"timeStamps,omitempty"`

	Encodings []encodingDto `json:"encodings"`
}

type timeStampDto struct {
	Ts   int    `json:"ts"`
	Name string `json:"name"`
}

type encodingDto struct {
	Name         string           `json:"name"`
	VideoSources []videoSourceDto `json:"videoSources"`
}

type videoSourceDto struct {
	Resolution int    `json:"resolution"`
	Url        string `json:"url"`
}

func buildVideoData(vd *library.VideoData, baseUrl string) (*videoDataDto, error) {
	videoId := vd.Id()
	if len(vd.SceneParts.Files) == 0 {
		return nil, fmt.Errorf("scene %s has no files", videoId)
	}

	dto := videoDataDto{
		Authorized:  "1",
		FullAccess:  true,
		Title:       vd.Title(),
		Id:          videoId,
		VideoLength: int(vd.SceneParts.Files[0].Duration),
		SkipIntro:   0,
	}

	paths := vd.SceneParts.Paths
	if paths != nil {
		if paths.Screenshot != nil && *paths.Screenshot != "" {
			dto.ThumbnailUrl = util.Ptr(heatmap.GetCoverUrl(baseUrl, videoId))
		}

		if paths.Preview != nil {
			dto.VideoPreview = util.Ptr(stash.ApiKeyed(*paths.Preview))
		}
	}

	setStreamSources(vd, &dto)
	setMarkers(vd, &dto)
	setFormat(&dto, library.ResolveFormat(config.Application().VideoRules, vd.SceneParts.Tags))

	return &dto, nil
}

func setStreamSources(vd *library.VideoData, dto *videoDataDto) {
	streams := []stash.Stream{stash.GetTranscodingStream(vd.SceneParts), stash.GetDirectStream(vd.SceneParts)}
	dto.Encodings = make([]encodingDto, len(streams))
	for i, stream := range streams {
		dto.Encodings[i] = encodingDto{
			Name:         stream.Name,
			VideoSources: make([]videoSourceDto, len(stream.Sources)),
		}
		for j, source := range stream.Sources {
			dto.Encodings[i].VideoSources[j] = videoSourceDto{
				Resolution: source.Resolution,
				Url:        source.Url,
			}
		}
	}
}

func setMarkers(vd *library.VideoData, dto *videoDataDto) {
	for _, sm := range vd.SceneParts.Scene_markers {
		sb := strings.Builder{}
		sb.WriteString(sm.Primary_tag.Name)
		if sm.Title != "" {
			sb.WriteString(":")
			sb.WriteString(sm.Title)
		}
		ts := timeStampDto{
			Ts:   int(sm.Seconds),
			Name: sb.String(),
		}
		dto.TimeStamps = append(dto.TimeStamps, ts)
	}
}

// setFormat maps the resolved rule format to DeoVR's screen and stereo
// vocabulary. DeoVR has no passthrough or cubemap modes; cubemaps fall
// back to a sphere.
func setFormat(dto *videoDataDto, f library.Format) {
	switch f.Projection {
	case "equirectangular":
		dto.ScreenType = "dome"
	case "equirectangular360", "cubemap", "equiangularCubemap":
		dto.ScreenType = "sphere"
	case "fisheye":
		switch {
		case f.Lens == "MKX200":
			dto.ScreenType = "mkx200"
		case f.Fov == 190:
			dto.ScreenType = "rf52"
		default:
			dto.ScreenType = "fisheye"
		}
	case "perspective":
		dto.ScreenType = "flat"
	}
	switch f.Stereo {
	case "sbs":
		dto.StereoMode = "sbs"
	case "tb":
		dto.StereoMode = "tb"
	case "mono":
		dto.StereoMode = "off"
	}
	if dto.ScreenType == "rf52" {
		dto.StereoMode = "cuv"
	}
	dto.Is3d = dto.ScreenType != "" || dto.StereoMode != ""
}
