package internal

import (
	"context"
	"testing"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

func TestCheckImportBlockedExclusionList(t *testing.T) {
	m := NewModule(Config{DBPath: t.TempDir() + "/m.db"})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	now := nowRFC()
	_, err := m.db.Exec(`INSERT INTO exclusion_lists (id, name, type, list_url, api_key, tmdb_ids_json, last_synced, created_at)
		VALUES ('ex1', 'Block 550', 'mdblist', '', '', '[550]', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := m.CheckImportBlocked(ctx, &maintainv1.CheckImportBlockedRequest{Scope: "movie", TmdbId: 550})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.GetBlocked() {
		t.Fatal("expected blocked")
	}
}
