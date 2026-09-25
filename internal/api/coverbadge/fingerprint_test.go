package coverbadge

import (
	"strings"
	"testing"

	"stash-vr/internal/config"
)

func TestFingerprint(t *testing.T) {
	rules := config.DefaultVideoRules()
	quality := config.CoverBadges{Quality: true}
	format := config.CoverBadges{Quality: true, Format: true}

	if got := Fingerprint(config.CoverBadges{}, rules); got != "" {
		t.Fatalf("all badges off must give no fingerprint, got %q", got)
	}
	q := Fingerprint(quality, rules)
	if q == "" || len(q) > 8 {
		t.Fatalf("expected a short fingerprint, got %q", q)
	}
	if Fingerprint(quality, rules) != q {
		t.Fatal("the same settings must give the same fingerprint")
	}
	if Fingerprint(format, rules) == q {
		t.Fatal("switching a badge on must change the fingerprint")
	}

	changed := config.DefaultVideoRules()
	changed[0].Projection = "fisheye"
	if Fingerprint(format, changed) == Fingerprint(format, rules) {
		t.Fatal("with the format badge on, a rule change must change the fingerprint")
	}
	if Fingerprint(quality, changed) != q {
		t.Fatal("the quality badge alone does not depend on the rules")
	}
}

func TestURLQuery(t *testing.T) {
	if got := URLQuery(config.CoverBadges{}, nil); got != "" {
		t.Fatalf("no badges must leave cover URLs as they were, got %q", got)
	}
	on := config.CoverBadges{Passthrough: true}
	got := URLQuery(on, config.DefaultVideoRules())
	if !strings.HasPrefix(got, "?b=") || got != "?b="+Fingerprint(on, config.DefaultVideoRules()) {
		t.Fatalf("expected ?b=<fingerprint>, got %q", got)
	}
}
