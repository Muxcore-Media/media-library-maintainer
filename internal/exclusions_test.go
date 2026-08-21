package internal

import "testing"

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
