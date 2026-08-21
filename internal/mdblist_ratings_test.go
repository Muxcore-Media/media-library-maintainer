package internal

import "testing"

func TestParseMDBListRatingsPayload(t *testing.T) {
	raw := []byte(`{"ratings":[
		{"source":"letterboxd","value":4.6,"votes":"1,200"},
		{"source":"trakt","score":82,"votes":5000}
	]}`)
	facts := parseMDBListRatingsPayload(raw)
	if facts.LetterboxdScore != 92 {
		t.Fatalf("letterboxd score %v", facts.LetterboxdScore)
	}
	if facts.LetterboxdVoteCount != 1200 {
		t.Fatalf("letterboxd votes %d", facts.LetterboxdVoteCount)
	}
	if facts.TraktRating != 82 {
		t.Fatalf("trakt %v", facts.TraktRating)
	}
}

func TestEvalUserScopedPlayback(t *testing.T) {
	values := map[string]float64{
		"alice": 90,
		"bob":   10,
	}
	ok, err := evalUserScopedPlayback(values, "gte", map[string]any{
		"usernames": []any{"Bob"},
		"amount":    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected bob below threshold")
	}
	ok, err = evalUserScopedPlayback(values, "gte", map[string]any{
		"usernames": []any{"alice", "bob"},
		"amount":    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected alice match")
	}
}

func TestWatchedPercent(t *testing.T) {
	if watchedPercent(progressEntry{PositionSec: 1800}, 7200) != 25 {
		t.Fatal("expected 25%")
	}
	if watchedPercent(progressEntry{Watched: true}, 0) != 100 {
		t.Fatal("expected watched=100%")
	}
}
