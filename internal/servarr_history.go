package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type historyRecord struct {
	EventType  string         `json:"eventType"`
	DownloadID string         `json:"downloadId"`
	EpisodeID  int            `json:"episodeId"`
	Data       map[string]any `json:"data"`
}

func (m *Module) resolveDownloadIDs(ctx context.Context, scope MediaScope, ec EvalContext) []string {
	switch scope {
	case ScopeMovie, ScopeMovieFile:
		if ec.TmdbID <= 0 {
			return nil
		}
		movieID, err := m.radarrMovieID(ctx, ec.TmdbID)
		if err != nil || movieID <= 0 {
			return nil
		}
		return m.servarrHistoryHashes(ctx, "radarr", fmt.Sprintf("/history/movie?movieId=%d", movieID))
	case ScopeSeries, ScopeSeason:
		if ec.TmdbID <= 0 {
			return nil
		}
		seriesID, err := m.sonarrSeriesID(ctx, ec.TmdbID)
		if err != nil || seriesID <= 0 {
			return nil
		}
		return m.servarrHistoryHashes(ctx, "sonarr", fmt.Sprintf("/history/series?seriesId=%d", seriesID))
	case ScopeEpisode:
		if ec.TmdbID <= 0 {
			return nil
		}
		seriesID, err := m.sonarrSeriesID(ctx, ec.TmdbID)
		if err != nil || seriesID <= 0 {
			return nil
		}
		epID, err := m.sonarrEpisodeID(ctx, seriesID, ec.SeasonNumber, ec.EpisodeNumber)
		if err != nil || epID <= 0 {
			return m.servarrHistoryHashes(ctx, "sonarr", fmt.Sprintf("/history/series?seriesId=%d", seriesID))
		}
		items := m.servarrHistoryRecords(ctx, "sonarr", fmt.Sprintf("/history/series?seriesId=%d", seriesID))
		var ids []string
		seen := map[string]struct{}{}
		for _, rec := range items {
			hash := downloadProducingHash(rec)
			if hash == "" || rec.EpisodeID != epID {
				continue
			}
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			ids = append(ids, hash)
		}
		return ids
	default:
		return nil
	}
}

func downloadProducingHash(rec historyRecord) string {
	eventType := strings.ToLower(rec.EventType)
	if eventType != "grabbed" && eventType != "downloadfolderimported" {
		return ""
	}
	if h := strings.TrimSpace(rec.DownloadID); h != "" {
		return strings.ToLower(h)
	}
	if rec.Data != nil {
		if v, ok := rec.Data["torrentInfoHash"].(string); ok {
			if h := strings.TrimSpace(v); h != "" {
				return strings.ToLower(h)
			}
		}
	}
	return ""
}

func (m *Module) servarrHistoryRecords(ctx context.Context, kind, path string) []historyRecord {
	base, key, err := m.servarrConfig(kind)
	if err != nil {
		return nil
	}
	raw, code, err := m.servarrRequest(ctx, base, key, http.MethodGet, "/api/v3"+path, nil)
	if err != nil || code != http.StatusOK {
		return nil
	}
	var records []historyRecord
	if json.Unmarshal(raw, &records) != nil {
		return nil
	}
	return records
}

func (m *Module) servarrHistoryHashes(ctx context.Context, kind, path string) []string {
	records := m.servarrHistoryRecords(ctx, kind, path)
	seen := map[string]struct{}{}
	var out []string
	for _, rec := range records {
		hash := downloadProducingHash(rec)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, hash)
	}
	return out
}

func (m *Module) servarrConfig(kind string) (base, key string, err error) {
	switch kind {
	case "radarr":
		base = os.Getenv("RADARR_URL")
		key = os.Getenv("RADARR_API_KEY")
	case "sonarr":
		base = os.Getenv("SONARR_URL")
		key = os.Getenv("SONARR_API_KEY")
	default:
		return "", "", fmt.Errorf("unknown servarr %s", kind)
	}
	if base == "" || key == "" {
		return "", "", fmt.Errorf("%s not configured", kind)
	}
	return strings.TrimRight(base, "/"), key, nil
}

func (m *Module) servarrRequest(ctx context.Context, base, key, method, path string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return raw, resp.StatusCode, err
}

func (m *Module) radarrMovieID(ctx context.Context, tmdbID int) (int, error) {
	base, key, err := m.servarrConfig("radarr")
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v3/movie?tmdbId=%d", tmdbID)
	raw, code, err := m.servarrRequest(ctx, base, key, http.MethodGet, path, nil)
	if err != nil || code != http.StatusOK {
		return 0, fmt.Errorf("radarr movie lookup status %d", code)
	}
	var movies []struct {
		ID int `json:"id"`
	}
	if json.Unmarshal(raw, &movies) != nil || len(movies) == 0 {
		return 0, fmt.Errorf("radarr movie not found")
	}
	return movies[0].ID, nil
}

func (m *Module) sonarrSeriesID(ctx context.Context, tmdbID int) (int, error) {
	base, key, err := m.servarrConfig("sonarr")
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v3/series?tmdbId=%d", tmdbID)
	raw, code, err := m.servarrRequest(ctx, base, key, http.MethodGet, path, nil)
	if err != nil || code != http.StatusOK {
		return 0, fmt.Errorf("sonarr series lookup status %d", code)
	}
	var series []struct {
		ID int `json:"id"`
	}
	if json.Unmarshal(raw, &series) != nil || len(series) == 0 {
		return 0, fmt.Errorf("sonarr series not found")
	}
	return series[0].ID, nil
}

func (m *Module) sonarrEpisodeID(ctx context.Context, seriesID, season, episode int) (int, error) {
	base, key, err := m.servarrConfig("sonarr")
	if err != nil {
		return 0, err
	}
	q := url.Values{}
	q.Set("seriesId", strconv.Itoa(seriesID))
	q.Set("seasonNumber", strconv.Itoa(season))
	raw, code, err := m.servarrRequest(ctx, base, key, http.MethodGet, "/api/v3/episode?"+q.Encode(), nil)
	if err != nil || code != http.StatusOK {
		return 0, fmt.Errorf("sonarr episodes status %d", code)
	}
	var episodes []struct {
		ID            int `json:"id"`
		EpisodeNumber int `json:"episodeNumber"`
	}
	if json.Unmarshal(raw, &episodes) != nil {
		return 0, fmt.Errorf("parse sonarr episodes")
	}
	for _, ep := range episodes {
		if ep.EpisodeNumber == episode {
			return ep.ID, nil
		}
	}
	return 0, fmt.Errorf("sonarr episode not found")
}

func (m *Module) cleanupDownloadClient(ctx context.Context, c storedCandidate) {
	if !m.getDownloadClientEnabled() {
		return
	}
	if !m.actionRemovesFiles(c.ArrAction) {
		return
	}
	ec := parseEvalContext(c.CriteriaJSON)
	ids := m.resolveDownloadIDs(ctx, c.Scope, ec)
	if len(ids) == 0 {
		return
	}
	dc := m.torrentCleanupClient()
	if dc == nil {
		return
	}
	if err := dc.removeDownloads(ctx, ids, m.getDownloadClientDeleteData(), m.getDownloadClientFallbackRatio()); err != nil {
		slog.Debug("download client cleanup", "item", c.ItemID, "error", err)
	}
}

func (m *Module) actionRemovesFiles(action ArrAction) bool {
	switch action {
	case ActionDelete, ActionUnmonitor, ActionRemoveIfEmpty:
		return true
	default:
		return false
	}
}
