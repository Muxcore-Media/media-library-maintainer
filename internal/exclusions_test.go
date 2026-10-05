package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsExcludedByList(t *testing.T) {
	m := NewModule(Config{})
	excluded := map[int]struct{}{42: {}}
	if !m.isExcludedByList(42, excluded) {
		t.Fatal("expected exclusion")
	}
	if m.isExcludedByList(99, excluded) {
		t.Fatal("unexpected exclusion")
	}
	if m.isExcludedByList(0, excluded) {
		t.Fatal("zero tmdb id should not match")
	}
}

func TestTraktListSlug(t *testing.T) {
	if got := traktListSlug("https://trakt.tv/users/demo/lists/watchlist"); got != "watchlist" {
		t.Fatalf("got %q", got)
	}
	if got := traktListSlug("lists/my-list"); got != "my-list" {
		t.Fatalf("got %q", got)
	}
}

func TestParseTMDBIDList(t *testing.T) {
	ids := parseTMDBIDList(`[1,2,3]`)
	if len(ids) != 3 || ids[0] != 1 {
		t.Fatalf("got %v", ids)
	}
}

// Regression: syncExclusionLists must not hold its SELECT cursor open while
// writing; the pool has a single connection, so that deadlocked.
func TestSyncExclusionListsDoesNotDeadlock(t *testing.T) {
	m := newTestModule(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tmdbid":11},{"tmdbid":22}]`))
	}))
	defer srv.Close()
	m.httpCli = srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range []string{"a", "b"} {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO exclusion_lists (id, name, type, list_url, created_at) VALUES (?, ?, 'mdblist', ?, ?)`,
			id, id, srv.URL, nowRFC()); err != nil {
			t.Fatal(err)
		}
	}

	type result struct {
		synced, loaded int
		err            error
	}
	done := make(chan result, 1)
	go func() {
		s, l, err := m.syncExclusionLists(ctx)
		done <- result{s, l, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.synced != 2 || r.loaded != 4 {
			t.Fatalf("synced=%d loaded=%d, want 2/4", r.synced, r.loaded)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("syncExclusionLists deadlocked")
	}

	var raw, last string
	if err := m.db.QueryRowContext(context.Background(), `SELECT tmdb_ids_json, last_synced FROM exclusion_lists WHERE id='a'`).Scan(&raw, &last); err != nil {
		t.Fatal(err)
	}
	if raw != "[11,22]" || last == "" {
		t.Fatalf("write not persisted: %q %q", raw, last)
	}
}
