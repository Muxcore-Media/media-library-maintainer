package internal

import (
	"image"
	"path/filepath"
	"testing"
	"time"
)

func TestRenderOverlayTemplateCountdownBar(t *testing.T) {
	m := NewModule(Config{})
	tpl := m.overlayTemplateForMode("poster")
	if tpl == nil {
		t.Fatal("expected default poster template")
	}
	src := image.NewRGBA(image.Rect(0, 0, 200, 300))
	actAfter := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	out := renderOverlayTemplate(src, tpl, overlayRenderContext{
		ActAfter: actAfter,
		Label:    "Leaving Soon",
		Style:    m.overlayStyle(),
	})
	if out == nil {
		t.Fatal("expected rendered image")
	}
	b := out.Bounds()
	if b.Dx() != 200 || b.Dy() != 300 {
		t.Fatalf("unexpected bounds %v", b)
	}
}

func TestResolveOverlayVariableDaysText(t *testing.T) {
	el := overlayElement{
		TextToday: "TODAY",
		TextDay:   "IN 1 DAY",
		TextDays:  "IN {0} DAYS",
	}
	ctx := overlayRenderContext{
		ActAfter: time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339),
	}
	got := resolveOverlayVariable("daysText", el, ctx)
	if got != "IN 3 DAYS" && got != "IN 2 DAYS" {
		t.Fatalf("unexpected daysText %q", got)
	}
}

func TestBitDepthFromPixelFormat(t *testing.T) {
	if bitDepthFromPixelFormat("yuv420p10le") != 10 {
		t.Fatal("expected 10-bit")
	}
	if bitDepthFromPixelFormat("yuv420p") != 8 {
		t.Fatal("expected 8-bit")
	}
}

func TestOverlayPrimaryArtworkEpisode(t *testing.T) {
	artType, tplMode := overlayPrimaryArtwork(ScopeEpisode)
	if artType != "still" || tplMode != "titlecard" {
		t.Fatalf("got %s/%s", artType, tplMode)
	}
}

func TestListPlaybackUsers(t *testing.T) {
	dir := t.TempDir()
	writeUserProgress(t, filepath.Join(dir, "alice.json"), "m1", 10, false, "2026-01-01T00:00:00Z")
	m := NewModule(Config{})
	m.cfgMu.Lock()
	m.userdataDataDir = dir
	m.cfgMu.Unlock()
	users := m.listPlaybackUsers("", 10)
	if len(users) != 1 || users[0] != "alice" {
		t.Fatalf("got %v", users)
	}
}
