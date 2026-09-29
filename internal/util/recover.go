package util

import (
	"context"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

// RecoverLog is deferred at the top of a goroutine that runs outside the
// HTTP handler chain (where chi's recoverer cannot catch it): a panic in
// such a goroutine is logged with what and its stack instead of taking the
// whole server down.
func RecoverLog(ctx context.Context, what string) {
	if r := recover(); r != nil {
		log.Ctx(ctx).Error().Interface("panic", r).Str("stack", string(debug.Stack())).Msgf("Recovered panic: %s", what)
	}
}
