package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWatchStatsAggregatesPlayback(t *testing.T) {
	dir := t.TempDir()
	userA := filepath.Join(dir, "user-a.json")
	userB := filepath.Join(dir, "user-b.json")
	itemID := "movie-1"
	writeUserProgress(t, userA, itemID, 3600, false, "2026-01-01T00:00:00Z")
	writeUserProgress(t, userB, itemID, 1800, true, "2026-01-15T00:00:00Z")

	m := NewModule(Config{})
	m.cfgMu.Lock()
	m.userdataDataDir = dir
	m.cfgMu.Unlock()

	ws := m.watchStatsFromUserdata(itemID, 120)
	if ws.UniqueUsers != 2 {
		t.Fatalf("unique users %d", ws.UniqueUsers)
	}
	if !ws.HasActivity {
		t.Fatal("expected activity")
	}
	if ws.TotalDurationMinutes != 90 {
		t.Fatalf("total minutes %v", ws.TotalDurationMinutes)
	}
	if ws.LongestDurationMinutes != 60 {
		t.Fatalf("longest minutes %v", ws.LongestDurationMinutes)
	}

	ec := EvalContext{}
	applyWatchStats(&ec, ws)
	if ec.PlaybackUniqueUsers != 2 || !ec.PlaybackHasActivity {
		t.Fatalf("applyWatchStats failed: %+v", ec)
	}
}

func writeUserProgress(t *testing.T, path, itemID string, positionSec float64, watched bool, updatedAt string) {
	t.Helper()
	raw := `{"progress":{"` + itemID + `":{"id":"` + itemID + `","positionSec":` + formatFloat(positionSec) + `,"watched":` + boolStr(watched) + `,"updatedAt":"` + updatedAt + `"}}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func formatFloat(v float64) string {
	return fmt.Sprintf("%g", v)
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
