package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type mdblistRatingFacts struct {
	LetterboxdScore     float64
	LetterboxdVoteCount int
	TraktRating         float64
	CriticScore         float64
}

func (m *Module) getMDBListAPIKey() string {
	if v := os.Getenv("MDBLIST_API_KEY"); v != "" {
		return v
	}
	return os.Getenv("MAINTAINER_MDBLIST_API_KEY")
}

func (m *Module) fetchMDBListRatings(ctx context.Context, ec *EvalContext) mdblistRatingFacts {
	apiKey := m.getMDBListAPIKey()
	if apiKey == "" || ec == nil || ec.TmdbID <= 0 {
		return mdblistRatingFacts{}
	}
	endpoint := "movie"
	if ec.Scope == ScopeSeries || ec.Scope == ScopeSeason || ec.Scope == ScopeEpisode {
		endpoint = "show"
	}
	base := os.Getenv("MDBLIST_BASE_URL")
	if base == "" {
		base = "https://api.mdblist.com"
	}
	base = strings.TrimRight(base, "/")
	url := fmt.Sprintf("%s/tmdb/%s/%d?apikey=%s", base, endpoint, ec.TmdbID, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) //nolint:gosec // mdblist base URL is operator-configured
	if err != nil {
		return mdblistRatingFacts{}
	}
	resp, err := m.httpCli.Do(req) //nolint:gosec // mdblist base URL is operator-configured
	if err != nil {
		return mdblistRatingFacts{}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mdblistRatingFacts{}
	}
	if resp.StatusCode != http.StatusOK {
		if endpoint == "show" && resp.StatusCode == http.StatusNotFound {
			return m.fetchMDBListRatingsTV(ctx, ec.TmdbID, apiKey, base)
		}
		return mdblistRatingFacts{}
	}
	return parseMDBListRatingsPayload(raw)
}

func (m *Module) fetchMDBListRatingsTV(ctx context.Context, tmdbID int, apiKey, base string) mdblistRatingFacts {
	url := fmt.Sprintf("%s/tmdb/tv/%d?apikey=%s", base, tmdbID, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) //nolint:gosec // mdblist base URL is operator-configured
	if err != nil {
		return mdblistRatingFacts{}
	}
	resp, err := m.httpCli.Do(req) //nolint:gosec // mdblist base URL is operator-configured
	if err != nil {
		return mdblistRatingFacts{}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return mdblistRatingFacts{}
	}
	return parseMDBListRatingsPayload(raw)
}

func parseMDBListRatingsPayload(raw []byte) mdblistRatingFacts {
	var payload struct {
		Ratings []map[string]any `json:"ratings"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return mdblistRatingFacts{}
	}
	facts := mdblistRatingFacts{}
	for _, item := range payload.Ratings {
		source := strings.ToLower(strings.TrimSpace(fmt.Sprint(item["source"])))
		score := mdblistScore(item, source)
		votes := mdblistVotes(item["votes"])
		switch source {
		case "letterboxd":
			facts.LetterboxdScore = score
			facts.LetterboxdVoteCount = votes
		case "trakt":
			if facts.TraktRating == 0 {
				facts.TraktRating = score
			}
		case "metacritic":
			if facts.CriticScore == 0 {
				facts.CriticScore = score
			}
		}
	}
	return facts
}

func mdblistScore(item map[string]any, source string) float64 {
	if v := parseRatingNumber(item["score"]); v > 0 {
		return v
	}
	value := parseRatingNumber(item["value"])
	if value <= 0 {
		return 0
	}
	if source == "letterboxd" && value <= 5 {
		return float64(int(value*20 + 0.5))
	}
	if value <= 1 {
		return value * 100
	}
	if value <= 10 {
		return value * 10
	}
	return value
}

func mdblistVotes(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		v = strings.ReplaceAll(v, ",", "")
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return 0
}

func parseRatingNumber(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "N/A") {
			return 0
		}
		if strings.Contains(v, "/") {
			parts := strings.SplitN(v, "/", 2)
			num, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			den, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 == nil && err2 == nil && den > 0 {
				return num / den * 100
			}
		}
		f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", ""), 64)
		if err == nil {
			return f
		}
	}
	return 0
}
