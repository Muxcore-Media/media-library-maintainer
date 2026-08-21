package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
)

type ratingFacts struct {
	ImdbRating  float64
	CriticScore float64
	TraktRating float64
	AnilistScore float64
	LetterboxdScore     float64
	LetterboxdVoteCount int
}

type ratingsCache struct {
	mu    sync.RWMutex
	items map[string]ratingFacts
}

var globalRatingsCache = ratingsCache{items: make(map[string]ratingFacts)}

func (m *Module) enrichRatings(ctx context.Context, ec *EvalContext) {
	if ec == nil {
		return
	}
	cacheKey := ratingCacheKey(ec)
	if cacheKey == "" {
		return
	}
	if ec.ImdbRating > 0 && ec.CriticScore > 0 && ec.TraktRating > 0 && ec.AnilistScore > 0 && ec.LetterboxdScore > 0 {
		return
	}
	globalRatingsCache.mu.RLock()
	cached, ok := globalRatingsCache.items[cacheKey]
	globalRatingsCache.mu.RUnlock()
	if ok {
		applyRatingFacts(ec, cached)
		return
	}
	facts := m.fetchRatingsForContext(ctx, ec)
	if facts.ImdbRating == 0 && facts.CriticScore == 0 && facts.TraktRating == 0 && facts.AnilistScore == 0 && facts.LetterboxdScore == 0 {
		return
	}
	globalRatingsCache.mu.Lock()
	globalRatingsCache.items[cacheKey] = facts
	globalRatingsCache.mu.Unlock()
	applyRatingFacts(ec, facts)
}

func ratingCacheKey(ec *EvalContext) string {
	if ec.ImdbID != "" {
		return ec.ImdbID
	}
	if ec.TmdbID > 0 {
		return fmt.Sprintf("%s:tmdb:%d", ec.Scope, ec.TmdbID)
	}
	return ""
}

func (m *Module) fetchRatingsForContext(ctx context.Context, ec *EvalContext) ratingFacts {
	facts := ratingFacts{}
	if ec.Scope == ScopeSeries || ec.Scope == ScopeSeason || ec.Scope == ScopeEpisode {
		if trakt := m.fetchTraktShowRating(ctx, ec.TmdbID); trakt > 0 {
			facts.TraktRating = trakt
		}
	} else {
		facts = m.fetchMovieRatings(ctx, ec.ImdbID, ec.TmdbID)
	}
	if isAnimeGenres(ec.Genres) {
		if anilist := m.fetchAniListFacts(ctx, ec); anilist.Score > 0 {
			facts.AnilistScore = anilist.Score
		}
	}
	mdb := m.fetchMDBListRatings(ctx, ec)
	if mdb.LetterboxdScore > 0 {
		facts.LetterboxdScore = mdb.LetterboxdScore
		facts.LetterboxdVoteCount = mdb.LetterboxdVoteCount
	}
	if facts.TraktRating == 0 && mdb.TraktRating > 0 {
		facts.TraktRating = mdb.TraktRating
	}
	if facts.CriticScore == 0 && mdb.CriticScore > 0 {
		facts.CriticScore = mdb.CriticScore
	}
	return facts
}

func (m *Module) fetchMovieRatings(ctx context.Context, imdbID string, tmdbID int) ratingFacts {
	facts := m.fetchOMDBRatings(ctx, imdbID)
	if trakt := m.fetchTraktMovieRating(ctx, imdbID, tmdbID); trakt > 0 {
		facts.TraktRating = trakt
	}
	return facts
}

func applyRatingFacts(ec *EvalContext, facts ratingFacts) {
	if ec.ImdbRating == 0 {
		ec.ImdbRating = facts.ImdbRating
	}
	if ec.CriticScore == 0 {
		ec.CriticScore = facts.CriticScore
	}
	if ec.TraktRating == 0 {
		ec.TraktRating = facts.TraktRating
	}
	if ec.AnilistScore == 0 {
		ec.AnilistScore = facts.AnilistScore
	}
	if ec.LetterboxdScore == 0 {
		ec.LetterboxdScore = facts.LetterboxdScore
	}
	if ec.LetterboxdVoteCount == 0 {
		ec.LetterboxdVoteCount = facts.LetterboxdVoteCount
	}
}

func (m *Module) getTraktClientID() string {
	if v := os.Getenv("TRAKT_CLIENT_ID"); v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_TRAKT_CLIENT_ID"); v != "" {
		return v
	}
	return os.Getenv("TRAKT_API_KEY")
}

func (m *Module) fetchTraktMovieRating(ctx context.Context, imdbID string, tmdbID int) float64 {
	ref := traktMovieRef(imdbID, tmdbID)
	if ref == "" {
		return 0
	}
	return m.fetchTraktRating(ctx, "movies", ref)
}

func (m *Module) fetchTraktShowRating(ctx context.Context, tmdbID int) float64 {
	if tmdbID <= 0 {
		return 0
	}
	return m.fetchTraktRating(ctx, "shows", fmt.Sprintf("tmdb:%d", tmdbID))
}

func (m *Module) fetchTraktRating(ctx context.Context, kind, ref string) float64 {
	clientID := m.getTraktClientID()
	if clientID == "" || ref == "" {
		return 0
	}
	endpoint := fmt.Sprintf("https://api.trakt.tv/%s/%s/ratings", kind, url.PathEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", clientID)
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return 0
	}
	var payload struct {
		Rating float64 `json:"rating"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Rating <= 0 {
		return 0
	}
	return payload.Rating * 10
}

func traktMovieRef(imdbID string, tmdbID int) string {
	if imdbID != "" {
		return imdbID
	}
	if tmdbID > 0 {
		return fmt.Sprintf("tmdb:%d", tmdbID)
	}
	return ""
}

func (m *Module) fetchOMDBRatings(ctx context.Context, imdbID string) ratingFacts {
	apiKey := os.Getenv("OMDB_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("MAINTAINER_OMDB_API_KEY")
	}
	if apiKey == "" || imdbID == "" {
		return ratingFacts{}
	}
	endpoint := fmt.Sprintf("https://www.omdbapi.com/?apikey=%s&i=%s", url.QueryEscape(apiKey), url.QueryEscape(imdbID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ratingFacts{}
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return ratingFacts{}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return ratingFacts{}
	}
	var payload struct {
		ImdbRating string `json:"imdbRating"`
		Metascore  string `json:"Metascore"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ratingFacts{}
	}
	return parseOMDBPayload(payload.ImdbRating, payload.Metascore)
}

func parseOMDBPayload(imdbRating, metascore string) ratingFacts {
	facts := ratingFacts{}
	if v, err := strconv.ParseFloat(strings.TrimSpace(imdbRating), 64); err == nil {
		facts.ImdbRating = v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(metascore), 64); err == nil {
		facts.CriticScore = v
	}
	return facts
}
