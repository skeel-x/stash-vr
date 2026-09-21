package logger

import (
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
)

// New builds the process logger and sets the effective log level through
// zerolog's global level. The logger itself is left at trace level so a later
// level change is a single atomic store rather than a logger swap, which
// would race with in-flight requests.
func New(level string, disableColor bool) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		panic(fmt.Sprintf("error parsing log level: %v", err))
	}
	zerolog.SetGlobalLevel(lvl)

	l := log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: "Jan 02, 15:04:05",
		NoColor:    disableColor,
	}).With().Str("mod", "default").Logger().Level(zerolog.TraceLevel) //.With().Caller().Logger()

	return l
}
