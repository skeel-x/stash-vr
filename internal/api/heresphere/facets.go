package heresphere

import (
	"fmt"
	"strconv"
	"time"

	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
)

const dateLayout = "2006-01-02"

// ageAt returns the whole years between a YYYY-MM-DD birthdate and at.
func ageAt(birthdate string, at time.Time) (int, bool) {
	b, err := time.Parse(dateLayout, birthdate)
	if err != nil {
		return 0, false
	}
	years := at.Year() - b.Year()
	// Compare month and day rather than day-of-year so a leap day between
	// the two dates does not shift the birthday.
	if at.Month() < b.Month() || (at.Month() == b.Month() && at.Day() < b.Day()) {
		years--
	}
	if years < 0 {
		return 0, false
	}
	return years, true
}

// performerFacets derives Country and Age tags from the scene's performers.
// Ages are taken at the scene's date when it has one, else at now. Repeated
// values (two performers from one country, or of one age) give one tag.
func performerFacets(vd *library.VideoData, now time.Time) []tagDto {
	at := now
	if vd.SceneParts.Date != nil {
		if d, err := time.Parse(dateLayout, *vd.SceneParts.Date); err == nil {
			at = d
		}
	}
	var tags []tagDto
	seen := map[string]struct{}{}
	add := func(name string) {
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		tags = append(tags, tagDto{Name: name})
	}
	for _, p := range vd.SceneParts.Performers {
		if p == nil {
			continue
		}
		if p.Country != nil && *p.Country != "" {
			add(fmt.Sprintf("%s%s%s", internal.LegendPerformerCountry, seperator, *p.Country))
		}
		if p.Birthdate != nil {
			if age, ok := ageAt(*p.Birthdate, at); ok {
				add(fmt.Sprintf("%s%s%s", internal.LegendPerformerAge, seperator, strconv.Itoa(age)))
			}
		}
	}
	return tags
}
