package internal

import "testing"

func TestProbeMediaFile(t *testing.T) {
	c, r, v := probeMediaFile("/movies/Foo.2020.x265.mkv", "Bluray-1080p", "")
	if c != "mkv" {
		t.Fatalf("container %q", c)
	}
	if r != "1080p" {
		t.Fatalf("resolution %q", r)
	}
	if v != "hevc" {
		t.Fatalf("codec %q", v)
	}
}

func TestPlaybackRuleFields(t *testing.T) {
	ctx := EvalContext{
		PlaybackPlayCount:              3,
		PlaybackUniqueUsers:            2,
		PlaybackTotalDurationMinutes:   95.5,
		PlaybackLongestDurationMinutes: 60,
		PlaybackHasActivity:            true,
		AnilistScore:                   88,
		MediaContainer:                 "mkv",
		VideoResolution:                "1080p",
	}
	def := RuleDefinition{
		Op: "and",
		Conditions: []RuleCondition{
			{Field: "playback.play_count", Operator: "greater_than", Value: 2},
			{Field: "playback.has_activity", Operator: "equals", Value: true},
			{Field: "anilist.score", Operator: "greater_than", Value: 80},
			{Field: "media.container", Operator: "contains_any", Value: []any{"mkv"}},
		},
	}
	ok, err := evaluateRule(def, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected playback rule match")
	}
}

func TestIsAnimeGenres(t *testing.T) {
	if !isAnimeGenres([]string{"Animation", "Sci-Fi"}) {
		t.Fatal("expected anime genre match")
	}
	if isAnimeGenres([]string{"Drama"}) {
		t.Fatal("expected non-anime")
	}
}
