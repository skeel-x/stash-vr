package stash

import (
	"context"
	"fmt"
	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/stash/gql"
	"strconv"
)

// GetMinPlayPercent reads Stash's minimumPlayPercent UI setting: the
// percentage of a scene's duration that has to be played for a play to
// count. A setting that is missing or cannot be parsed reads as 0; an error
// means Stash could not be asked, so the caller keeps what it had.
func GetMinPlayPercent(ctx context.Context, client graphql.Client) (float64, error) {
	configurationResponse, err := gql.UIConfiguration(ctx, client)
	if err != nil {
		return 0, fmt.Errorf("UIConfiguration: %w", err)
	}

	minPlayPercent := configurationResponse.Configuration.Ui["minimumPlayPercent"]
	if minPlayPercent == nil {
		return 0, nil
	}

	switch v := minPlayPercent.(type) {
	case float64:
		return v, nil
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Interface("config.minimumPlayPercent", minPlayPercent).Msg("Failed to parse Stash config.minimumPlayPercent")
			return 0, nil
		}
		return parsed, nil
	default:
		log.Ctx(ctx).Warn().Interface("config.minimumPlayPercent", minPlayPercent).Msg("Failed to parse Stash config.minimumPlayPercent: Unsupported format")
		return 0, nil
	}
}
