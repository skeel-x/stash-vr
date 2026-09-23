package heresphere

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"net/http"
	"net/url"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"strings"
	"time"
)

type httpHandler struct {
	libraryService *library.Service
	ps             *playbackState
}

var minPlayFraction *float64

func (h *httpHandler) indexHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	baseUrl := internal.GetBaseUrl(req)

	mpf := stash.GetMinPlayPercent(ctx, h.libraryService.Client()) / 100
	minPlayFraction = &mpf

	sections, err := h.libraryService.GetSectionsFor(ctx, "heresphere")
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to get sections")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	go func() {
		ctx := context.Background()
		_, err := h.libraryService.GetScenes(ctx)
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to get scenes")
		}
	}()

	dto, err := buildIndex(sections, baseUrl)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to build index")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := internal.WriteJson(ctx, w, dto); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("write")
	}
}

func (h *httpHandler) scanHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	baseUrl := internal.GetBaseUrl(req)

	vds, err := h.libraryService.GetScenes(ctx)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to get scenes")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	dto, err := buildScan(ctx, vds, baseUrl)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to build scan")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := internal.WriteJson(ctx, w, dto); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("write")
	}
}

// maxVideoDataBody bounds the scene request body: a profile write-back is
// at most MaxProfileBytes once decoded, plus base64 overhead and the
// other fields.
const maxVideoDataBody = library.MaxProfileBytes*4/3 + 64*1024

func (h *httpHandler) videoDataHandler(w http.ResponseWriter, req *http.Request) {
	defer func() { _ = req.Body.Close() }()
	req.Body = http.MaxBytesReader(w, req.Body, maxVideoDataBody)

	ctx := req.Context()
	baseUrl := internal.GetBaseUrl(req)
	videoId, err := url.QueryUnescape(chi.URLParam(req, "videoId"))
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("malformed videoId")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if vdReq, err := internal.UnmarshalBody[videoDataRequestDto](req); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to parse request body")
	} else {
		if vdReq.DeleteFile != nil && *vdReq.DeleteFile {
			if err = h.libraryService.Delete(ctx, videoId); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msg("Failed to delete scene")
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		go h.processUpdates(videoId, vdReq)
	}

	vd, err := h.libraryService.GetScene(ctx, videoId, false)
	if err != nil {
		if errors.Is(err, library.ErrSceneNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Ctx(ctx).Error().Err(err).Msg("failed to get scene")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	dto, err := buildVideoData(ctx, vd, baseUrl, h.libraryService.ScriptVariants(ctx, videoId), h.libraryService)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to build video data")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := internal.WriteJson(ctx, w, dto); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("write")
	}
}

func (h *httpHandler) processUpdates(videoId string, vdReq videoDataRequestDto) {
	ctx := context.Background()
	needsRefetch := false
	if vdReq.Rating != nil {
		if err := h.libraryService.UpdateRating(ctx, videoId, vdReq.Rating); err != nil {
			log.Ctx(ctx).Warn().Err(err).Float32("rating", *vdReq.Rating).Msg("Failed to update rating")
		}
		needsRefetch = true
	}
	if vdReq.IsFavorite != nil {
		if err := h.libraryService.UpdateFavorite(ctx, videoId, *vdReq.IsFavorite); err != nil {
			log.Ctx(ctx).Warn().Err(err).Bool("isFavorite", *vdReq.IsFavorite).Msg("Failed to update favorite")
		}
		needsRefetch = true
	}
	if vdReq.Tags != nil {
		h.processIncomingTags(ctx, videoId, vdReq)
		needsRefetch = true
	}
	if vdReq.Hsp != nil && *vdReq.Hsp != "" {
		h.saveProfile(ctx, videoId, *vdReq.Hsp)
	}
	if needsRefetch {
		_, err := h.libraryService.GetScene(ctx, videoId, true)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to refetch scene")
		}
	}
}

// saveProfile stores the profile HereSphere sent back for the scene.
func (h *httpHandler) saveProfile(ctx context.Context, videoId, encoded string) {
	if len(encoded) > library.MaxProfileBytes*4/3+4 {
		log.Ctx(ctx).Warn().Str("scene", videoId).Msg("Ignoring oversized HereSphere profile")
		return
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", videoId).Msg("Ignoring malformed HereSphere profile")
		return
	}
	if err := h.libraryService.SaveProfile(videoId, data); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", videoId).Msg("Failed to store HereSphere profile")
		return
	}
	log.Ctx(ctx).Info().Str("scene", videoId).Int("bytes", len(data)).Msg("Stored HereSphere profile")
}

func (h *httpHandler) processIncomingTags(ctx context.Context, videoId string, vdReq videoDataRequestDto) {
	in := classifyIncomingTags(ctx, *vdReq.Tags)

	for _, c := range in.commands {
		switch c {
		case strings.ToLower(internal.CommandIncrementO):
			if err := h.libraryService.IncrementO(ctx, videoId); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msg("Failed to increment O")
			}
		case strings.ToLower(internal.CommandSetOrganizedTrue):
			if err := h.libraryService.SetOrganized(ctx, videoId, true); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msg("Failed to set organized=true")
			}
		}
	}

	if !in.hasPlayCount {
		if err := h.libraryService.DecrementPlayCount(ctx, videoId); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to decrement play count")
		}
	}

	if !in.hasOrganized {
		if err := h.libraryService.SetOrganized(ctx, videoId, false); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to set organized=false")
		}
	}

	if !in.hasOCount {
		if err := h.libraryService.DecrementO(ctx, videoId); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to decrement O")
		}
	}

	if !in.hasRating {
		if err := h.libraryService.UpdateRating(ctx, videoId, nil); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to set zero rating")
		}
	}

	if err := h.libraryService.UpdateTags(ctx, videoId, in.tags); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to update tags")
	}

	if err := h.libraryService.UpdateMarkers(ctx, videoId, in.markers); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to update markers")
	}
}

func (h *httpHandler) eventsHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	ev, err := internal.UnmarshalBody[playbackEvent](req)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to parse event body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	parts := strings.Split(ev.Id, "/")
	videoId := parts[len(parts)-1]
	vd, err := h.libraryService.GetScene(ctx, videoId, false)
	if err != nil {
		if errors.Is(err, library.ErrSceneNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to get scene from event")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Ctx(ctx).Debug().Str("id", ev.Id).Str("event", ev.Event.String()).Send()

	switch ev.Event {
	case evPlay:
		if h.ps == nil {
			h.ps = newPlayback(vd)
		} else if h.ps.videoId != videoId {
			// Another scene started: report what was played of the previous
			// one; its resume position is unknown here, so leave it as is.
			played := h.ps.handleStop(ctx, h.libraryService, minPlayFraction)
			h.saveActivity(h.ps.videoId, playedPtr(played), nil)
			h.ps = newPlayback(vd)
		} else {
			h.ps.handleResume()
		}
	case evPause, evClose:
		var played *float64
		if h.ps != nil {
			played = playedPtr(h.ps.handleStop(ctx, h.libraryService, minPlayFraction))
		}
		if len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil {
			return
		}
		resume := resumePosition(vd.SceneParts.Files[0].Duration, float64(ev.Time))
		h.saveActivity(vd.Id(), played, &resume)
	default:
	}
}

// saveActivity reports a playback stop to Stash on a detached context so a
// player quitting mid-request cannot cancel the write. played is the seconds
// played since the last report (nil when nothing was played); resume is the
// position to resume from (nil leaves Stash's stored position unchanged).
func (h *httpHandler) saveActivity(id string, played *float64, resume *float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.libraryService.SaveActivity(ctx, id, played, resume); err != nil {
		log.Warn().Err(err).Str("id", id).Msg("Failed to save playback activity")
		return
	}
	log.Debug().Str("id", id).Msg("Saved playback activity")
}

func playedPtr(seconds float64) *float64 {
	if seconds <= 0 {
		return nil
	}
	return &seconds
}

type videoDataRequestDto struct {
	Rating           *float32  `json:"rating,omitempty"`
	IsFavorite       *bool     `json:"isFavorite,omitempty"`
	Tags             *[]tagDto `json:"tags,omitempty"`
	DeleteFile       *bool     `json:"deleteFile,omitempty"`
	NeedsMediaSource *bool     `json:"needsMediaSource,omitempty"`
	Hsp              *string   `json:"hsp,omitempty"`
}
