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
