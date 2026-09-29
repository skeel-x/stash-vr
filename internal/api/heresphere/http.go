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
	"stash-vr/internal/hsp"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
	"strings"
	"sync/atomic"
	"time"
)

type httpHandler struct {
	libraryService *library.Service
	// playback tracks what every client is playing; see playbackTracker.
	playback *playbackTracker
	// sceneLocks serialises the write-backs of one scene: HereSphere
	// sends a scene request per change, and two of them overlapping read
	// the same cached markers and tags, so both created the same marker
	// and the later one undid the earlier one's tag edits.
	sceneLocks util.KeyedMutex
	// minPlayFraction is the fraction of a scene's duration that counts
	// as a play, from Stash's minimumPlayPercent; nil until it was read.
	// Index requests refresh it and events read it, concurrently.
	minPlayFraction atomic.Pointer[float64]
}

func newHttpHandler(libraryService *library.Service) *httpHandler {
	return &httpHandler{libraryService: libraryService, playback: newPlaybackTracker()}
}

// refreshMinPlayFraction reads minimumPlayPercent from Stash. When Stash
// cannot be asked the value already held stays, so a blip does not turn
// every stop into a counted play.
func (h *httpHandler) refreshMinPlayFraction(ctx context.Context) *float64 {
	pct, err := stash.GetMinPlayPercent(ctx, h.libraryService.Client())
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Failed to read Stash's minimum play percent")
		return h.minPlayFraction.Load()
	}
	mpf := pct / 100
	h.minPlayFraction.Store(&mpf)
	return &mpf
}

// minPlayFractionFor is the fraction for a playback stop: the one read on
// the last index build, else read now, so a play reported before any
// index was built since the start (a headset resuming after a restart)
// still counts.
func (h *httpHandler) minPlayFractionFor(ctx context.Context) *float64 {
	if p := h.minPlayFraction.Load(); p != nil {
		return p
	}
	return h.refreshMinPlayFraction(ctx)
}

const (
	// prefetchTimeout bounds the scene prefetch an index request starts. It
	// runs detached from the request, so a headset dropping the connection
	// does not abort it, and bounded, so a silent Stash cannot pin it until
	// the process restarts.
	prefetchTimeout = 5 * time.Minute
	// updateTimeout bounds the write-back of one scene's rating, tags and
	// favourite state plus the refetch that follows, for the same reasons.
	updateTimeout = time.Minute
)

func (h *httpHandler) indexHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	baseUrl := internal.GetBaseUrl(req)

	h.refreshMinPlayFraction(ctx)

	sections, err := h.libraryService.GetSectionsFor(ctx, "heresphere")
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to get sections")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), prefetchTimeout)
		defer cancel()
		defer util.RecoverLog(ctx, "prefetch scenes for the index")
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

	vdReq, reqErr := internal.UnmarshalBody[videoDataRequestDto](req)
	if reqErr != nil {
		log.Ctx(ctx).Warn().Err(reqErr).Msg("Failed to parse request body")
	} else if vdReq.DeleteFile != nil && *vdReq.DeleteFile {
		if err = h.libraryService.Delete(ctx, videoId); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to delete scene")
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	// Resolve the scene before acting on the request: updates for a scene
	// that does not exist are not worth a background job.
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
	if reqErr == nil {
		go h.processUpdates(ctx, videoId, vdReq)
	}

	if vd.ReleaseDate() == "" {
		// Look the date up ahead of the sweep so the next open shows it.
		h.libraryService.RequestDate(videoId)
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

// processUpdates writes the request's changes to Stash. It runs after the
// response is sent, detached from the request's cancellation but bounded by
// updateTimeout; reqCtx is the request context it keeps the values of.
// Updates of the same scene run one at a time, in the order they get the
// lock; the timeout starts once the update holds it.
func (h *httpHandler) processUpdates(reqCtx context.Context, videoId string, vdReq videoDataRequestDto) {
	defer h.sceneLocks.Lock(videoId)()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), updateTimeout)
	defer cancel()
	defer util.RecoverLog(ctx, "process updates for scene "+videoId)
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
	log.Ctx(ctx).Info().Str("scene", videoId).Int("encodedBytes", len(encoded)).Msg("Received HereSphere profile")
	if len(encoded) > library.MaxProfileBytes*4/3+4 {
		log.Ctx(ctx).Warn().Str("scene", videoId).Msg("Ignoring oversized HereSphere profile")
		return
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", videoId).Msg("Ignoring malformed HereSphere profile")
		return
	}
	// A payload that does not decode as a profile would replace a good
	// stored one with something HereSphere cannot load either.
	if err := hsp.Validate(data); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", videoId).Msg("Ignoring HereSphere profile that does not decode")
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

	key := clientKey(req, &ev)
	log.Ctx(ctx).Debug().Str("id", ev.Id).Str("event", ev.Event.String()).Str("client", key).Send()

	switch ev.Event {
	case evPlay:
		if prev := h.playback.play(key, vd, h.minPlayFractionFor(ctx)); prev != nil {
			h.reportStop(ctx, prev, nil)
		}
	case evPause, evClose:
		stop := h.playback.stop(key, videoId, h.minPlayFractionFor(ctx))
		if len(vd.SceneParts.Files) == 0 || vd.SceneParts.Files[0] == nil {
			if stop != nil {
				h.reportStop(ctx, stop, nil)
			}
			return
		}
		resume := resumePosition(vd.SceneParts.Files[0].Duration, float64(ev.Time))
		if stop == nil {
			// The client was not playing this scene here (a stop without a
			// start, or for another scene): nothing was played, but the
			// position is still worth keeping.
			stop = &playbackStop{videoId: vd.Id()}
		}
		h.reportStop(ctx, stop, &resume)
	default:
	}
}

// reportStop writes one playback stop to Stash: the play count when the
// stop crossed the threshold, then the seconds played and the resume
// position (nil leaves Stash's stored position unchanged).
func (h *httpHandler) reportStop(ctx context.Context, stop *playbackStop, resume *float64) {
	if stop.countPlay {
		log.Ctx(ctx).Debug().Str("id", stop.videoId).Msg("Incrementing play count")
		if err := h.libraryService.IncrementPlayCount(ctx, stop.videoId); err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("id", stop.videoId).Msg("Failed to increment play count")
		}
	}
	h.saveActivity(stop.videoId, playedPtr(stop.played), resume)
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
