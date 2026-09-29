package stash

import (
	"context"
	"fmt"
	"github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog/log"
	"stash-vr/internal/stash/gql"
	"strconv"
)

func FindSavedFilterIdsByFrontPage(ctx context.Context, client graphql.Client) ([]string, error) {
	configurationResponse, err := gql.UIConfiguration(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("UIConfiguration: %w", err)
	}

	return frontPageFilterIds(ctx, configurationResponse.Configuration.Ui["frontPageContent"]), nil
}

// frontPageFilterIds picks the saved filter ids out of Stash's
// frontPageContent UI setting. Entries that are not objects, are not saved
// filters, or carry no usable savedFilterId are skipped, never fatal.
func frontPageFilterIds(ctx context.Context, frontPageContent any) []string {
	if frontPageContent == nil {
		log.Ctx(ctx).Info().Msg("No frontpage content found")
		return nil
	}

	frontPageFilters, ok := frontPageContent.([]interface{})
	if !ok {
		log.Ctx(ctx).Warn().Str("type", fmt.Sprintf("%T", frontPageContent)).Msg("Frontpage content is not a list, ignoring")
		return nil
	}
	filterIds := make([]string, 0, len(frontPageFilters))
	for _, _filter := range frontPageFilters {
		filter, ok := _filter.(map[string]interface{})
		if !ok {
			log.Ctx(ctx).Debug().Str("type", fmt.Sprintf("%T", _filter)).Msg("Filter skipped: frontpage entry is not an object")
			continue
		}
		typeName, ok := filter["__typename"].(string)
		if !ok {
			log.Ctx(ctx).Debug().Msg("Filter skipped: frontpage entry has no __typename")
			continue
		}
		if typeName != "SavedFilter" {
			log.Ctx(ctx).Debug().Str("type", typeName).Msg("Filter skipped: Unsupported filter type on front page: Only user created SCENE filters are supported.")
			continue
		}
		filterId, ok := savedFilterId(filter["savedFilterId"])
		if !ok {
			log.Ctx(ctx).Debug().Interface("savedFilterId", filter["savedFilterId"]).Msg("Filter skipped: frontpage entry has no usable savedFilterId")
			continue
		}
		filterIds = append(filterIds, filterId)
	}

	return filterIds
}

// savedFilterId reads a saved filter id, which Stash stores either as a
// string or as a JSON number.
func savedFilterId(v any) (string, bool) {
	switch id := v.(type) {
	case string:
		return id, id != ""
	case float64:
		return strconv.Itoa(int(id)), true
	default:
		return "", false
	}
}
