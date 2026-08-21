package internal

import (
	"context"
	"testing"
)

func TestParseOMDBPayload(t *testing.T) {
	facts := parseOMDBPayload("8.7", "73")
	if facts.ImdbRating != 8.7 || facts.CriticScore != 73 {
		t.Fatalf("unexpected facts: %+v", facts)
	}
}

func TestEnrichRatingsCache(t *testing.T) {
	globalRatingsCache.mu.Lock()
	globalRatingsCache.items = map[string]ratingFacts{
		"tt0133093": {ImdbRating: 8.7, CriticScore: 73},
	}
	globalRatingsCache.mu.Unlock()

	m := newTestModule(t)
	ec := &EvalContext{ImdbID: "tt0133093"}
	m.enrichRatings(context.Background(), ec)
	if ec.ImdbRating != 8.7 || ec.CriticScore != 73 {
		t.Fatalf("ratings not enriched: imdb=%v critic=%v", ec.ImdbRating, ec.CriticScore)
	}
}
