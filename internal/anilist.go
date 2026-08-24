package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type anilistFacts struct {
	Score      float64
	Popularity int
	Favourites int
}

func (m *Module) fetchAniListFacts(ctx context.Context, ec *EvalContext) anilistFacts {
	if ec == nil || ec.TmdbID <= 0 {
		return anilistFacts{}
	}
	mediaType := "ANIME"
	if ec.Scope == ScopeMovie {
		mediaType = "ANIME"
	}
	query := `query ($tmdb: Int, $type: MediaType) {
  Page(page: 1, perPage: 1) {
    media(tmdbId: $tmdb, type: $type) {
      averageScore
      popularity
      favourites
    }
  }
}`
	payload := map[string]any{
		"query": query,
		"variables": map[string]any{
			"tmdb": ec.TmdbID,
			"type": mediaType,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return anilistFacts{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://graphql.anilist.co", bytes.NewReader(body))
	if err != nil {
		return anilistFacts{}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return anilistFacts{}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return anilistFacts{}
	}
	var parsed struct {
		Data struct {
			Page struct {
				Media []struct {
					AverageScore *int `json:"averageScore"`
					Popularity   int  `json:"popularity"`
					Favourites   int  `json:"favourites"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &parsed) != nil || len(parsed.Data.Page.Media) == 0 {
		return anilistFacts{}
	}
	media := parsed.Data.Page.Media[0]
	facts := anilistFacts{
		Popularity: media.Popularity,
		Favourites: media.Favourites,
	}
	if media.AverageScore != nil && *media.AverageScore > 0 {
		facts.Score = float64(*media.AverageScore)
	}
	return facts
}

func isAnimeGenres(genres []string) bool {
	for _, g := range genres {
		gl := strings.ToLower(strings.TrimSpace(g))
		if gl == "animation" || gl == "anime" {
			return true
		}
	}
	return false
}
