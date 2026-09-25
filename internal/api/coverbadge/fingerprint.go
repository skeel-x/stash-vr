package coverbadge

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"

	"stash-vr/internal/config"
)

// fingerprintMemo keeps the last fingerprint: cover URLs are built per
// scene, and the settings rarely change. rules is the first rule of the
// slice it was computed for; config.Set always stores a fresh slice, and
// holding the pointer keeps that slice from being reused.
var fingerprintMemo struct {
	sync.Mutex
	on    config.CoverBadges
	rules *config.VideoRule
	n     int
	value string
}

// Fingerprint is a short hash of the badge settings and, when the format
// or passthrough badge is on, the video rules those badges resolve
// through; "" when every badge is off. Cover URLs carry it so headsets,
// which keep covers for a day, fetch them again when it changes.
func Fingerprint(on config.CoverBadges, rules []config.VideoRule) string {
	if !on.Any() {
		return ""
	}
	var first *config.VideoRule
	if len(rules) > 0 {
		first = &rules[0]
	}
	m := &fingerprintMemo
	m.Lock()
	defer m.Unlock()
	if m.value != "" && m.on == on && m.rules == first && m.n == len(rules) {
		return m.value
	}
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "q%t f%t p%t", on.Quality, on.Format, on.Passthrough)
	if on.Format || on.Passthrough {
		b, _ := json.Marshal(rules)
		_, _ = h.Write(b)
	}
	m.on, m.rules, m.n = on, first, len(rules)
	m.value = fmt.Sprintf("%08x", h.Sum32())
	return m.value
}

// URLQuery is "?b=<fingerprint>" for cover URLs, or "" with every badge
// off so the URLs stay as they were.
func URLQuery(on config.CoverBadges, rules []config.VideoRule) string {
	if f := Fingerprint(on, rules); f != "" {
		return "?b=" + f
	}
	return ""
}

// CurrentURLQuery is URLQuery for the current settings.
func CurrentURLQuery() string {
	cfg := config.Application()
	return URLQuery(cfg.CoverBadges, cfg.VideoRules)
}
