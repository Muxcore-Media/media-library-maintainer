package internal

import (
	"image/color"
	"testing"
	"time"
)

func TestNormalizeRuleFieldAliases(t *testing.T) {
	cases := map[string]string{
		"trakt.rating":                    "ratings.trakt",
		"seerr.requested":                 "request.is_requested",
		"movie.version_count":             "media.version_count",
		"tmdb.days_since_first_air_date":  "tmdb.days_since_first_air_date",
	}
	for in, want := range cases {
		if got := normalizeRuleField(in); got != want {
			t.Fatalf("%s -> %q want %q", in, got, want)
		}
	}
}

func TestReclaimerrAliasEvaluation(t *testing.T) {
	ctx := EvalContext{
		Requested:         true,
		RequesterWatched:  false,
		MovieVersionCount: 2,
		RuntimeMinutes:    120,
		TraktRating:       82,
	}
	def := RuleDefinition{
		Op: "and",
		Conditions: []RuleCondition{
			{Field: "seerr.requested", Operator: "equals", Value: true},
			{Field: "movie.version_count", Operator: "greater_than", Value: 1},
			{Field: "trakt.rating", Operator: "greater_than", Value: 80},
		},
	}
	ok, err := evaluateRule(def, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected alias rule match")
	}
}

func TestParseOverlayColorHex(t *testing.T) {
	c := parseOverlayColor("#ff0000", color.RGBA{})
	if c.R != 255 || c.G != 0 || c.B != 0 {
		t.Fatalf("unexpected color %+v", c)
	}
}

func TestFormatOverlaySubtitleDate(t *testing.T) {
	actAfter := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	got := formatOverlaySubtitleDate(actAfter, "Jan 2")
	if got != "AUG 27" {
		t.Fatalf("got %q", got)
	}
}
