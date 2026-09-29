package coverbadge

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"

	"stash-vr/internal/config"
)

// LayoutVersion numbers where and how badges are drawn. It is part of the
// fingerprint, so a new layout gives new cover URLs and headsets drop the
// covers they keep. 2: bottom left, above the heatmap strip.
const LayoutVersion = 2

// fingerprintMemo keeps the last fingerprint: cover URLs are built per
// scene, and the settings rarely change. rules is the first rule of the
// slice it was computed for; config.Set always stores a fresh slice, and
// holding the pointer keeps that slice from being reused.
var fingerprintMemo struct {
	sync.Mutex
	on     config.CoverBadges
	rules  *config.VideoRule
	n      int
	height int
	value  string
}

// Fingerprint is a short hash of everything that changes how a cover is
// drawn: the badge layout, the badge settings, the heatmap height and,
// when the format or passthrough badge is on, the video rules those
// badges resolve through. Cover URLs carry it so headsets, which keep
// covers for a day, fetch them again when it changes. It is never empty:
// the heatmap height matters with every badge off too.
func Fingerprint(on config.CoverBadges, rules []config.VideoRule, heatmapHeight int) string {
	var first *config.VideoRule
	if len(rules) > 0 {
		first = &rules[0]
	}
	m := &fingerprintMemo
	m.Lock()
	defer m.Unlock()
	if m.value != "" && m.on == on && m.rules == first && m.n == len(rules) && m.height == heatmapHeight {
		return m.value
	}
	m.on, m.rules, m.n, m.height = on, first, len(rules), heatmapHeight
	m.value = fingerprint(on, rules, heatmapHeight, LayoutVersion)
	return m.value
}

// fingerprint hashes the badge layout version, the switches, the heatmap
// height and, when the format or passthrough badge is on, the rules.
func fingerprint(on config.CoverBadges, rules []config.VideoRule, heatmapHeight int, layout int) string {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "l%d q%t f%t p%t d%t r%t h%d", layout, on.Quality, on.Format, on.Passthrough, on.Duration, on.FrameRate, heatmapHeight)
	if on.Format || on.Passthrough {
		b, _ := json.Marshal(rules)
		_, _ = h.Write(b)
	}
	return fmt.Sprintf("%08x", h.Sum32())
}

// URLQuery is "?b=<fingerprint>" for cover URLs.
func URLQuery(on config.CoverBadges, rules []config.VideoRule, heatmapHeight int) string {
	return "?b=" + Fingerprint(on, rules, heatmapHeight)
}

// CurrentURLQuery is URLQuery for the current settings.
func CurrentURLQuery() string {
	cfg := config.Application()
	return URLQuery(cfg.CoverBadges, cfg.VideoRules, cfg.HeatmapHeightPx)
}
