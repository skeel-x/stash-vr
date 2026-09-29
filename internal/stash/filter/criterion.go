package filter

import (
	"fmt"
	"stash-vr/internal/stash/gql"
	"strconv"
	"strings"
)

// decodeSimple reads a criterion whose value is a plain string ("true",
// "false" or free text) into dst.
func decodeSimple[T string | bool](c map[string]any, dst **T) error {
	x, ok := c["value"].(string)
	if !ok {
		return fmt.Errorf("value is %T, expected string", c["value"])
	}
	switch any(*dst).(type) {
	case *string:
		*dst = any(&x).(*T)
	case *bool:
		b, err := strconv.ParseBool(x)
		if err != nil {
			return fmt.Errorf("value %q is not a bool", x)
		}
		*dst = any(&b).(*T)
	}
	return nil
}

func modifier(c map[string]any) (gql.CriterionModifier, error) {
	m, ok := c["modifier"].(string)
	if !ok {
		return "", fmt.Errorf("modifier is %T, expected string", c["modifier"])
	}
	return gql.CriterionModifier(m), nil
}

// ids reads the "id" of every item in the list at path, or nil when the
// list is absent.
func ids(c map[string]any, path string) ([]string, error) {
	items := Get[[]any](c, path)
	if items == nil {
		return nil, nil
	}
	out := make([]string, len(*items))
	for i, o := range *items {
		id, err := require[string](o, "id")
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", path, i, err)
		}
		out[i] = id
	}
	return out, nil
}

func parseIntCriterionInput(c map[string]any) (*gql.IntCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.IntCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	out.Value = GetOr[int](c, "value.value", 0)
	out.Value2 = Get[int](c, "value.value2")
	return &out, nil
}

func parseHierarchicalMultiCriterionInput(c map[string]any) (*gql.HierarchicalMultiCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.HierarchicalMultiCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	out.Depth = Get[int](c, "value.depth")
	if out.Value, err = ids(c, "value.items"); err != nil {
		return nil, err
	}
	if out.Excludes, err = ids(c, "value.excluded"); err != nil {
		return nil, err
	}
	return &out, nil
}

func parseMultiCriterionInput(c map[string]any) (*gql.MultiCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.MultiCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	if out.Excludes, err = ids(c, "value.excluded"); err != nil {
		return nil, err
	}
	if out.Value, err = ids(c, "value.items"); err != nil {
		return nil, err
	}
	if out.Value == nil {
		if out.Value, err = ids(c, "value"); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

func parseTimestampCriterionInput(c map[string]any) (*gql.TimestampCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.TimestampCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	if out.Value, err = require[string](c, "value.value"); err != nil {
		return nil, err
	}
	out.Value2 = Get[string](c, "value.value2")
	return &out, nil
}

func parseDateCriterionInput(c map[string]any) (*gql.DateCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.DateCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	if out.Value, err = require[string](c, "value.value"); err != nil {
		return nil, err
	}
	out.Value2 = Get[string](c, "value.value2")
	return &out, nil
}

func parsePhashDistanceCriterionInput(c map[string]any) (*gql.PhashDistanceCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.PhashDistanceCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	if out.Value, err = require[string](c, "value.value"); err != nil {
		return nil, err
	}
	out.Distance = Get[int](c, "value.distance")
	return &out, nil
}

func parseResolutionCriterionInput(c map[string]any) (*gql.ResolutionCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.ResolutionCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}

	value, err := require[string](c, "value")
	if err != nil {
		return nil, err
	}
	switch value {
	case "144p":
		out.Value = gql.ResolutionEnumVeryLow
	case "240p":
		out.Value = gql.ResolutionEnumLow
	case "360p":
		out.Value = gql.ResolutionEnumR360p
	case "480p":
		out.Value = gql.ResolutionEnumStandard
	case "540p":
		out.Value = gql.ResolutionEnumWebHd
	case "720p":
		out.Value = gql.ResolutionEnumStandardHd
	case "1080p":
		out.Value = gql.ResolutionEnumFullHd
	case "1440p":
		out.Value = gql.ResolutionEnumQuadHd
	case "1920p":
		out.Value = gql.ResolutionEnumVrHd
	case "4k":
		out.Value = gql.ResolutionEnumFourK
	case "5k":
		out.Value = gql.ResolutionEnumFiveK
	case "6k":
		out.Value = gql.ResolutionEnumSixK
	case "8k":
		out.Value = gql.ResolutionEnumEightK
	case "Huge":
		out.Value = gql.ResolutionEnumHuge
	}

	return &out, nil
}

func parseStashIDCriterionInput(c map[string]any) (*gql.StashIDCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.StashIDCriterionInput{Modifier: mod}
	out.Endpoint = Get[string](c, "value.endpoint")
	out.Stash_id = Get[string](c, "value.stashID")
	return &out, nil
}

func parseDuplicationCriterionInput(c map[string]any) (*gql.DuplicationCriterionInput, error) {
	out := gql.DuplicationCriterionInput{}

	out.Phash = Get[bool](c, "value.phash")
	out.Stash_id = Get[bool](c, "value.stash_id")
	out.Title = Get[bool](c, "value.title")
	out.Url = Get[bool](c, "value.url")

	return &out, nil
}

func parseStringCriterionInput(c map[string]any) (*gql.StringCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.StringCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	if out.Value, err = require[string](c, "value"); err != nil {
		return nil, err
	}
	return &out, nil
}

func parseCaptionCriterionInput(c map[string]any) (*gql.StringCriterionInput, error) {
	mod, err := modifier(c)
	if err != nil {
		return nil, err
	}
	out := gql.StringCriterionInput{Modifier: mod}
	if out.Modifier == gql.CriterionModifierIsNull {
		return &out, nil
	}
	value, err := require[string](c, "value")
	if err != nil {
		return nil, err
	}
	switch value {
	case "Deutsche":
		out.Value = "de"
	case "English":
		out.Value = "en"
	case "Español":
		out.Value = "es"
	case "Français":
		out.Value = "fr"
	case "Italiano":
		out.Value = "it"
	case "日本":
		out.Value = "ja"
	case "한국인":
		out.Value = "ko"
	case "Holandés":
		out.Value = "nl"
	case "Português":
		out.Value = "pt"
	case "Русский":
		out.Value = "ru"
	case "Unknown":
		out.Value = "00"
	}
	return &out, nil
}

func parseOrientationCriterionInput(c map[string]any) (*gql.OrientationCriterionInput, error) {
	out := gql.OrientationCriterionInput{}

	values := Get[[]any](c, "value")
	if values == nil {
		return nil, fmt.Errorf("value is %T, expected list", c["value"])
	}
	out.Value = make([]gql.OrientationEnum, len(*values))
	for i, v := range *values {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("value[%d] is %T, expected string", i, v)
		}
		out.Value[i] = gql.OrientationEnum(strings.ToUpper(s))
	}
	return &out, nil
}
