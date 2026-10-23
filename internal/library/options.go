package library

import "stash-vr/internal/config"

// sectionOptions returns the settings saved for the section id, zero when
// the row has none. The override list is short (one row per section), so
// a scan per call is cheaper than keeping an index in step with it.
func sectionOptions(id string) config.SectionOptions {
	for _, f := range config.Application().Filters {
		if f.ID == id && f.Options != nil {
			return *f.Options
		}
	}
	return config.SectionOptions{}
}

// sectionSize is how many scenes the section shows: its own size, else the
// global default.
func sectionSize(id string, global int) int {
	return intOr(sectionOptions(id).Size, global)
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}
