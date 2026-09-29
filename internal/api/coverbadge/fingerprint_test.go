package coverbadge

import (
	"strings"
	"testing"

	"stash-vr/internal/config"
)

func TestFingerprint(t *testing.T) {
	rules := config.DefaultVideoRules()
	off := config.CoverBadges{}
	quality := config.CoverBadges{Quality: true}
	format := config.CoverBadges{Quality: true, Format: true}

	plain := Fingerprint(off, rules, 0)
	if plain == "" || len(plain) > 8 {
		t.Fatalf("every badge off still gives a short fingerprint, got %q", plain)
	}
	q := Fingerprint(quality, rules, 0)
	if q == "" || len(q) > 8 || q == plain {
		t.Fatalf("expected a short fingerprint of its own, got %q", q)
	}
	if Fingerprint(quality, rules, 0) != q {
		t.Fatal("the same settings must give the same fingerprint")
	}
	if Fingerprint(format, rules, 0) == q {
		t.Fatal("switching a badge on must change the fingerprint")
	}

	changed := config.DefaultVideoRules()
	changed[0].Projection = "fisheye"
	if Fingerprint(format, changed, 0) == Fingerprint(format, rules, 0) {
		t.Fatal("with the format badge on, a rule change must change the fingerprint")
	}
	if Fingerprint(quality, changed, 0) != q {
		t.Fatal("the quality badge alone does not depend on the rules")
	}
}

func TestFingerprint_HeatmapHeight(t *testing.T) {
	rules := config.DefaultVideoRules()
	for name, on := range map[string]config.CoverBadges{"badges off": {}, "quality": {Quality: true}} {
		t.Run(name, func(t *testing.T) {
			at0, at20, at40 := Fingerprint(on, rules, 0), Fingerprint(on, rules, 20), Fingerprint(on, rules, 40)
			if at0 == at20 || at20 == at40 || at0 == at40 {
				t.Fatalf("a heatmap height change must change the fingerprint: %s %s %s", at0, at20, at40)
			}
			if Fingerprint(on, rules, 20) != at20 || Fingerprint(on, rules, 0) != at0 {
				t.Fatal("the fingerprint must come back to the same value for the same height")
			}
		})
	}
}

func TestURLQuery(t *testing.T) {
	off := URLQuery(config.CoverBadges{}, nil, 0)
	if !strings.HasPrefix(off, "?b=") || off != "?b="+Fingerprint(config.CoverBadges{}, nil, 0) {
		t.Fatalf("with every badge off the cover URLs still carry the fingerprint, got %q", off)
	}
	on := config.CoverBadges{Passthrough: true}
	got := URLQuery(on, config.DefaultVideoRules(), 0)
	if !strings.HasPrefix(got, "?b=") || got != "?b="+Fingerprint(on, config.DefaultVideoRules(), 0) || got == off {
		t.Fatalf("expected ?b=<fingerprint>, got %q", got)
	}
	if URLQuery(config.CoverBadges{}, nil, 30) == off {
		t.Fatal("the heatmap height must reach the cover URLs with every badge off")
	}
}

func TestFingerprint_CarriesTheLayoutVersion(t *testing.T) {
	on := config.CoverBadges{Quality: true}
	rules := config.DefaultVideoRules()

	if fingerprint(on, rules, 0, 1) == fingerprint(on, rules, 0, 2) {
		t.Fatal("a new badge layout must change the fingerprint")
	}
	if Fingerprint(on, rules, 0) != fingerprint(on, rules, 0, LayoutVersion) {
		t.Fatal("the fingerprint must be taken with the current layout version")
	}
	if LayoutVersion < 2 {
		t.Fatal("badges moved to the bottom left corner in layout 2")
	}
}

func TestFingerprint_DurationAndFrameRate(t *testing.T) {
	rules := config.DefaultVideoRules()
	base := config.CoverBadges{Quality: true}
	q := Fingerprint(base, rules, 0)

	withDuration := base
	withDuration.Duration = true
	withRate := base
	withRate.FrameRate = true
	d, r := Fingerprint(withDuration, rules, 0), Fingerprint(withRate, rules, 0)
	if d == q || r == q || d == r {
		t.Fatalf("the duration and frame rate switches must change the fingerprint: %s %s %s", q, d, r)
	}
	off := Fingerprint(config.CoverBadges{}, rules, 0)
	if Fingerprint(config.CoverBadges{Duration: true}, rules, 0) == off || Fingerprint(config.CoverBadges{FrameRate: true}, rules, 0) == off {
		t.Fatal("either badge alone must change the fingerprint")
	}
}
