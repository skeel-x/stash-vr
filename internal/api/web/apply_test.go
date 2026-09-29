package web

import (
	"testing"

	"stash-vr/internal/api/coverbadge"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
)

func TestApplyChanges_BadgeChangeDropsRenderedCovers(t *testing.T) {
	t.Cleanup(coverbadge.ResetCache)
	lib := library.NewService(&fakeStash{})
	prev := config.ApplicationConfig{CoverBadges: config.CoverBadges{Quality: true}}

	coverbadge.Rendered.Add("k", []byte("jpeg"))
	ApplyChanges(prev, prev, lib)
	if coverbadge.Rendered.Len() != 1 {
		t.Fatal("unchanged badges must keep the rendered covers")
	}

	next := prev
	next.CoverBadges.Format = true
	ApplyChanges(prev, next, lib)
	if coverbadge.Rendered.Len() != 0 {
		t.Fatal("a badge change must drop the rendered covers")
	}
}

func TestApplyChanges_HeatmapHeightChangeDropsRenderedCovers(t *testing.T) {
	t.Cleanup(coverbadge.ResetCache)
	lib := library.NewService(&fakeStash{})
	prev := config.ApplicationConfig{HeatmapHeightPx: 20}

	coverbadge.Rendered.Add("k", []byte("jpeg"))
	ApplyChanges(prev, prev, lib)
	if coverbadge.Rendered.Len() != 1 {
		t.Fatal("an unchanged heatmap height must keep the rendered covers")
	}

	next := prev
	next.HeatmapHeightPx = 40
	ApplyChanges(prev, next, lib)
	if coverbadge.Rendered.Len() != 0 {
		t.Fatal("a heatmap height change must drop the rendered covers, which carry the strip at the old height")
	}
}
