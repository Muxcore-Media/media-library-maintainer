package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
)

type tvFileDetails struct {
	Path      string
	Quality   string
	Container string
	SizeBytes int64
}

func (m *Module) episodeFileDetails(ctx context.Context, tmdbSeriesID, seasonNum, episodeNum int, episodeID string) tvFileDetails {
	if tmdbSeriesID > 0 && seasonNum > 0 && episodeNum > 0 {
		if f, ok := m.sonarrEpisodeFile(ctx, tmdbSeriesID, seasonNum, episodeNum); ok {
			return f
		}
	}
	return m.tvEpisodeFileFromShow(ctx, episodeID)
}

func (m *Module) sonarrEpisodeFile(ctx context.Context, tmdbSeriesID, seasonNum, episodeNum int) (tvFileDetails, bool) {
	seriesID, err := m.sonarrSeriesID(ctx, tmdbSeriesID)
	if err != nil || seriesID <= 0 {
		return tvFileDetails{}, false
	}
	epID, err := m.sonarrEpisodeID(ctx, seriesID, seasonNum, episodeNum)
	if err != nil || epID <= 0 {
		return tvFileDetails{}, false
	}
	base, key, err := m.servarrConfig("sonarr")
	if err != nil {
		return tvFileDetails{}, false
	}
	q := url.Values{}
	q.Set("episodeId", strconv.Itoa(epID))
	raw, code, err := m.servarrRequest(ctx, base, key, "GET", "/api/v3/episodefile?"+q.Encode(), nil)
	if err != nil || code != 200 || len(raw) == 0 {
		return tvFileDetails{}, false
	}
	var files []struct {
		Path    string `json:"path"`
		Quality struct {
			Quality struct {
				Name string `json:"name"`
			} `json:"quality"`
		} `json:"quality"`
		Size int64 `json:"size"`
	}
	if json.Unmarshal(raw, &files) != nil || len(files) == 0 {
		return tvFileDetails{}, false
	}
	f := files[0]
	return tvFileDetails{
		Path:      f.Path,
		Quality:   f.Quality.Quality.Name,
		SizeBytes: f.Size,
	}, true
}

func (m *Module) tvEpisodeFileFromShow(ctx context.Context, episodeID string) tvFileDetails {
	if episodeID == "" {
		return tvFileDetails{}
	}
	if err := m.ensureTV(ctx); err != nil {
		return tvFileDetails{}
	}
	m.mu.RLock()
	tc := m.tvClient
	m.mu.RUnlock()
	if tc == nil {
		return tvFileDetails{}
	}
	// Walk series list is expensive; callers should prefer Sonarr or populated criteria paths.
	_ = tc
	return tvFileDetails{}
}

func (m *Module) seasonEpisodeFiles(ctx context.Context, c storedCandidate) ([]tvFileDetails, error) {
	ec := parseEvalContext(c.CriteriaJSON)
	if err := m.ensureTV(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	tc := m.tvClient
	m.mu.RUnlock()
	seriesID := ec.SeriesID
	if seriesID == "" {
		return m.seasonFilesFromSonarr(ctx, ec.TmdbID, ec.SeasonNumber)
	}
	resp, err := tc.GetTVShow(ctx, &tvmgmtv1.GetTVShowRequest{SeriesId: seriesID})
	if err != nil {
		return nil, err
	}
	var out []tvFileDetails
	for _, season := range resp.GetSeries().GetSeasons() {
		if ec.SeasonNumber > 0 && int(season.GetSeasonNumber()) != ec.SeasonNumber {
			continue
		}
		for _, ep := range season.GetEpisodes() {
			if !ep.GetHasFile() {
				continue
			}
			f := m.episodeFileDetails(ctx, ec.TmdbID, int(ep.GetSeasonNumber()), int(ep.GetEpisodeNumber()), ep.GetId())
			if f.Path != "" {
				out = append(out, f)
			}
		}
	}
	return out, nil
}

func (m *Module) seasonFilesFromSonarr(ctx context.Context, tmdbSeriesID, seasonNum int) ([]tvFileDetails, error) {
	if tmdbSeriesID <= 0 || seasonNum <= 0 {
		return nil, fmt.Errorf("season file lookup requires tmdb id and season number")
	}
	seriesID, err := m.sonarrSeriesID(ctx, tmdbSeriesID)
	if err != nil {
		return nil, err
	}
	base, key, err := m.servarrConfig("sonarr")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("seriesId", strconv.Itoa(seriesID))
	q.Set("seasonNumber", strconv.Itoa(seasonNum))
	raw, code, err := m.servarrRequest(ctx, base, key, "GET", "/api/v3/episode?"+q.Encode(), nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("sonarr episode list status %d", code)
	}
	var episodes []struct {
		SeasonNumber  int  `json:"seasonNumber"`
		EpisodeNumber int  `json:"episodeNumber"`
		HasFile       bool `json:"hasFile"`
	}
	if json.Unmarshal(raw, &episodes) != nil {
		return nil, fmt.Errorf("parse sonarr episodes")
	}
	var out []tvFileDetails
	for _, ep := range episodes {
		if !ep.HasFile {
			continue
		}
		if f, ok := m.sonarrEpisodeFile(ctx, tmdbSeriesID, ep.SeasonNumber, ep.EpisodeNumber); ok {
			out = append(out, f)
		}
	}
	return out, nil
}
