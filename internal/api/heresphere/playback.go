package heresphere

import (
	"context"
	"stash-vr/internal/library"
	"time"

	"github.com/rs/zerolog/log"
)

func newPlayback(vd *library.VideoData) *playbackState {
	return &playbackState{
		videoId:       vd.Id(),
		videoDuration: vd.SceneParts.Files[0].Duration,
		lastPlayTime:  time.Now(),
		isPlaying:     true,
	}
}

func (ps *playbackState) handleStop(ctx context.Context, libraryService *library.Service, minPlayFraction *float64) {
	if ps.isPlaying {
		currentPlayDuration := time.Since(ps.lastPlayTime)
		ps.accumulatedPlayTime += currentPlayDuration
		if !ps.thresholdReached && minPlayFraction != nil && ps.accumulatedPlayTime.Seconds() >= ps.videoDuration*(*minPlayFraction) {
			ps.thresholdReached = true
			log.Ctx(ctx).Debug().Str("total play time", ps.accumulatedPlayTime.Round(time.Second).String()).Msg("Incrementing play count")
			err := libraryService.IncrementPlayCount(ctx, ps.videoId)
			if err != nil {
				log.Ctx(ctx).Warn().Err(err).Msg("Failed to increment play count")
			}
		}
		log.Ctx(ctx).Debug().Str("duration", currentPlayDuration.Round(time.Second).String()).Msg("Adding play duration")
		err := libraryService.AddPlayDuration(ctx, ps.videoId, currentPlayDuration)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to add play duration")
		}
	}
	ps.isPlaying = false
}

// resumePosition decides what to store as the scene's resume time after the
// player paused or closed at position seconds. Positions inside the first 5
// seconds or at or beyond 97 percent of the duration clear the stored
// position, so finished scenes leave "Continue watching".
func resumePosition(duration, position float64) float64 {
	if duration <= 0 || position < 5 || position >= duration*0.97 {
		return 0
	}
	return position
}

func (ps *playbackState) handleResume() {
	if !ps.isPlaying {
		ps.lastPlayTime = time.Now()
	}
	ps.isPlaying = true
}

type playbackState struct {
	videoId       string
	videoDuration float64

	accumulatedPlayTime time.Duration
	thresholdReached    bool
	lastPlayTime        time.Time
	isPlaying           bool
}
