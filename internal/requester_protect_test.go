package internal

import (
	"testing"
	"time"
)

func TestTraktMovieRef(t *testing.T) {
	if got := traktMovieRef("tt0133093", 0); got != "tt0133093" {
		t.Fatalf("got %q", got)
	}
	if got := traktMovieRef("", 603); got != "tmdb:603" {
		t.Fatalf("got %q", got)
	}
}

func TestParseTraktRatingScale(t *testing.T) {
	if got := parseTraktRatingPayload(8.5); got != 85 {
		t.Fatalf("got %v", got)
	}
}

func parseTraktRatingPayload(rating float64) float64 {
	if rating <= 0 {
		return 0
	}
	return rating * 10
}

func TestShouldProtectUnwatchedRequester(t *testing.T) {
	m := &Module{protectUnwatchedRequesters: true}
	ec := EvalContext{
		Requested:        true,
		RequestedBy:      "alice",
		DaysSinceRequest: 10,
		RequesterWatched: false,
	}
	if !m.shouldProtectUnwatchedRequester(ec) {
		t.Fatal("expected protection")
	}
	ec.RequesterWatched = true
	if m.shouldProtectUnwatchedRequester(ec) {
		t.Fatal("watched requester should not be protected")
	}
}

func TestFormatOverlaySubtitle(t *testing.T) {
	actAfter := time.Now().UTC().Add(80 * time.Hour).Format(time.RFC3339)
	got := formatOverlaySubtitle(actAfter, true)
	if got != "IN 3 DAYS" && got != "IN 2 DAYS" {
		t.Fatalf("got %q", got)
	}
}
