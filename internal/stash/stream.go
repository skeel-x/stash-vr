package stash

import (
	"fmt"
	"regexp"
	"slices"
	"stash-vr/internal/stash/gql"
	"strconv"
	"strings"
)

type Stream struct {
	Name    string
	Sources []Source
}

type Source struct {
	Resolution int
	Url        string
}

var rgxResolution = regexp.MustCompile(`\((\d+)p\)`)

// fileHeight is the height of the scene's first file, 0 when the scene
// has none (a file gone missing, or a partial answer from Stash).
func fileHeight(sp *gql.SceneParts) int {
	if sp == nil || len(sp.Files) == 0 || sp.Files[0] == nil {
		return 0
	}
	return sp.Files[0].Height
}

// GetDirectStream is Stash's direct stream of the scene's file. A scene
// without a file or a stream path has none: the stream is returned with
// no sources rather than a nil dereference.
func GetDirectStream(sp *gql.SceneParts) Stream {
	direct := Stream{Name: "direct"}
	if sp == nil || len(sp.Files) == 0 || sp.Files[0] == nil ||
		sp.Paths == nil || sp.Paths.Stream == nil || *sp.Paths.Stream == "" {
		return direct
	}
	direct.Sources = []Source{{
		Resolution: sp.Files[0].Height,
		Url:        *sp.Paths.Stream,
	}}
	return direct
}

func GetTranscodingStream(sp *gql.SceneParts) Stream {
	return transcodingStream(sp, "video/mp4", "transcoding")
}

// GetHLSStream returns stash's segmented HLS transcodes, for players that
// cannot start on the mp4 transcode. See buildPlayableLinkCandidates in
// internal/api/playa for why PLAYA needs these.
func GetHLSStream(sp *gql.SceneParts) Stream {
	return transcodingStream(sp, "application/vnd.apple.mpegurl", "hls")
}

// transcodingStream lists the scene's transcodes whose mime type starts
// with mimePrefix, highest resolution first, one per resolution. Stream
// entries Stash left incomplete (no mime type or label) are skipped.
func transcodingStream(sp *gql.SceneParts, mimePrefix string, name string) Stream {
	mp4Sources := make([]Source, 0)
	if sp == nil {
		return Stream{Name: name, Sources: mp4Sources}
	}
	seenResolutions := make(map[int]struct{})
	for _, stream := range sp.SceneStreams {
		if stream == nil || stream.Mime_type == nil || stream.Label == nil {
			continue
		}
		if strings.HasPrefix(*stream.Mime_type, mimePrefix) && *stream.Label != "Direct stream" {
			resolution, err := parseResolutionFromLabel(*stream.Label)
			if err != nil {
				resolution = fileHeight(sp)
			}
			if _, seen := seenResolutions[resolution]; seen {
				continue
			}
			mp4Sources = append(mp4Sources, Source{
				Resolution: resolution,
				Url:        stream.Url,
			})
			seenResolutions[resolution] = struct{}{}
		}
	}
	slices.SortFunc(mp4Sources, func(a, b Source) int { return b.Resolution - a.Resolution })

	return Stream{
		Name:    name,
		Sources: mp4Sources,
	}
}

func parseResolutionFromLabel(label string) (int, error) {
	match := rgxResolution.FindStringSubmatch(label)
	if len(match) < 2 {
		return 0, fmt.Errorf("no resolution height found in label")
	}
	res, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("atoi: %w", err)
	}
	return res, nil
}
