package internal

import (
	"testing"
)

func TestDownloadProducingHash(t *testing.T) {
	rec := historyRecord{EventType: "grabbed", DownloadID: "ABC123"}
	if got := downloadProducingHash(rec); got != "abc123" {
		t.Fatalf("got %q", got)
	}
	rec = historyRecord{
		EventType: "downloadFolderImported",
		Data:      map[string]any{"torrentInfoHash": "DeF456"},
	}
	if got := downloadProducingHash(rec); got != "def456" {
		t.Fatalf("got %q", got)
	}
	if got := downloadProducingHash(historyRecord{EventType: "downloadFailed", DownloadID: "x"}); got != "" {
		t.Fatalf("expected empty for failed event")
	}
}

func TestShouldRemoveTorrent(t *testing.T) {
	if !shouldRemoveTorrent(qbittorrentTorrent{MaxRatio: 1, Ratio: 1.2}, 0.5) {
		t.Fatal("expected ratio limit met")
	}
	if shouldRemoveTorrent(qbittorrentTorrent{MaxRatio: -1, MaxSeedingTime: -1, Ratio: 0.2}, 0.5) {
		t.Fatal("expected below fallback ratio")
	}
	if !shouldRemoveTorrent(qbittorrentTorrent{MaxRatio: -1, MaxSeedingTime: -1, Ratio: 0.6}, 0.5) {
		t.Fatal("expected fallback ratio met")
	}
}

func TestActionRemovesFiles(t *testing.T) {
	m := &Module{}
	if !m.actionRemovesFiles(ActionDelete) {
		t.Fatal("delete should remove files")
	}
	if m.actionRemovesFiles(ActionMove) {
		t.Fatal("move should not trigger download cleanup")
	}
}
