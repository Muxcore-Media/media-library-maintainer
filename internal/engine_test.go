package internal

import (
	"testing"
	"time"
)

func TestEvaluateRuleAND(t *testing.T) {
	def := RuleDefinition{
		Op: "and",
		Conditions: []RuleCondition{
			{Field: "watch.never_watched", Operator: "equals", Value: true},
			{Field: "media.days_since_added", Operator: "greater_than", Value: 30},
		},
	}
	ctx := EvalContext{
		NeverWatched: true,
		AddedAt:      time.Now().UTC().Add(-60 * 24 * time.Hour),
	}
	ok, err := evaluateRule(def, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}
}

func TestEvaluateRuleOR(t *testing.T) {
	def := RuleDefinition{
		Op: "or",
		Conditions: []RuleCondition{
			{Field: "media.genres", Operator: "contains_any", Value: "Horror"},
			{Field: "media.title", Operator: "contains", Value: "test"},
		},
	}
	ctx := EvalContext{Title: "My Test Movie", Genres: []string{"Drama"}}
	ok, err := evaluateRule(def, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected title match")
	}
}

func TestMergeArrActionConservative(t *testing.T) {
	got := mergeArrAction(ActionUnmonitorOnly, ActionDelete)
	if got != ActionDelete {
		t.Fatalf("got %q want delete", got)
	}
	got = mergeArrAction(ActionDelete, ActionUnmonitorOnly)
	if got != ActionDelete {
		t.Fatalf("got %q want delete", got)
	}
}

func TestEvalGenreContainsAny(t *testing.T) {
	ok, err := evalGenre([]string{"Action", "Sci-Fi"}, "contains_any", "sci-fi")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected genre match")
	}
}

func TestDaysSinceAdded(t *testing.T) {
	ctx := EvalContext{AddedAt: time.Now().UTC().Add(-10 * 24 * time.Hour)}
	def := RuleDefinition{
		Op: "and",
		Conditions: []RuleCondition{
			{Field: "days_since_added", Operator: "greater_than", Value: 5},
		},
	}
	ok, err := evaluateRule(def, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected days_since_added match")
	}
}

func TestRatingFieldAliases(t *testing.T) {
	ctx := EvalContext{VoteAverage: 7.5, ImdbRating: 8.1, CriticScore: 85, TraktRating: 72}
	fields := []string{"media.vote_average", "vote_average", "media.rating", "ratings.tmdb", "ratings.vote_average"}
	for _, field := range fields {
		def := RuleDefinition{
			Op:         "and",
			Conditions: []RuleCondition{{Field: field, Operator: "greater_than", Value: 7.0}},
		}
		ok, err := evaluateRule(def, ctx)
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		if !ok {
			t.Fatalf("expected %s match", field)
		}
	}
	def := RuleDefinition{
		Op: "and",
		Conditions: []RuleCondition{
			{Field: "ratings.imdb", Operator: "greater_than", Value: 8.0},
			{Field: "ratings.metacritic", Operator: "greater_than", Value: 80},
			{Field: "ratings.trakt", Operator: "greater_than", Value: 70},
		},
	}
	ok, err := evaluateRule(def, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ratings fields match")
	}
}
