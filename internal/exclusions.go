package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func (m *Module) loadExclusionTMDBSet(ctx context.Context) map[int]struct{} {
	out := make(map[int]struct{})
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return out
	}
	rows, err := db.QueryContext(ctx, `SELECT tmdb_ids_json FROM exclusion_lists`)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var ids []int
		if json.Unmarshal([]byte(raw), &ids) == nil {
			for _, id := range ids {
				out[id] = struct{}{}
			}
		}
	}
	return out
}

func (m *Module) isExcludedByList(tmdbID int, excluded map[int]struct{}) bool {
	if tmdbID <= 0 {
		return false
	}
	_, ok := excluded[tmdbID]
	return ok
}

func (m *Module) syncExclusionLists(ctx context.Context) (listsSynced, idsLoaded int, err error) {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return 0, 0, fmt.Errorf("db unavailable")
	}
	rows, err := db.QueryContext(ctx, `SELECT id, name, type, list_url, api_key FROM exclusion_lists`)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, name, typ, url, key string
		if err := rows.Scan(&id, &name, &typ, &url, &key); err != nil {
			continue
		}
		ids, syncErr := m.fetchListTMDBIDs(ctx, typ, url, key)
		if syncErr != nil {
			continue
		}
		if typ == "justwatch" {
			listsSynced++
			continue
		}
		raw, _ := json.Marshal(ids)
		now := nowRFC()
		_, _ = db.ExecContext(ctx, `UPDATE exclusion_lists SET tmdb_ids_json = ?, last_synced = ? WHERE id = ?`, string(raw), now, id)
		listsSynced++
		idsLoaded += len(ids)
	}
	return listsSynced, idsLoaded, nil
}

func (m *Module) fetchListTMDBIDs(ctx context.Context, typ, listURL, apiKey string) ([]int, error) {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "trakt":
		return fetchTraktListTMDB(ctx, m.httpCli, listURL, apiKey)
	case "mdblist":
		return fetchMDBListTMDB(m.httpCli, listURL, apiKey)
	case "justwatch":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported list type %q", typ)
	}
}

func fetchTraktListTMDB(ctx context.Context, cli *http.Client, listURL, apiKey string) ([]int, error) {
	if apiKey == "" {
		apiKey = os.Getenv("TRAKT_CLIENT_ID")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("trakt client id required")
	}
	slug := traktListSlug(listURL)
	if slug == "" {
		return nil, fmt.Errorf("invalid trakt list url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.trakt.tv/lists/"+slug+"/items?type=movie,show", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", apiKey)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trakt API %d", resp.StatusCode)
	}
	var items []struct {
		Movie *struct {
			IDs struct {
				TMDB int `json:"tmdb"`
			} `json:"ids"`
		} `json:"movie"`
		Show *struct {
			IDs struct {
				TMDB int `json:"tmdb"`
			} `json:"ids"`
		} `json:"show"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	var ids []int
	seen := make(map[int]struct{})
	for _, it := range items {
		var id int
		switch {
		case it.Movie != nil:
			id = it.Movie.IDs.TMDB
		case it.Show != nil:
			id = it.Show.IDs.TMDB
		}
		if id > 0 {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

func traktListSlug(listURL string) string {
	listURL = strings.TrimSpace(listURL)
	if listURL == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(listURL, "/"), "/")
	for i, p := range parts {
		if p == "lists" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	if len(parts) >= 2 && parts[0] == "lists" {
		return parts[1]
	}
	return parts[len(parts)-1]
}

func fetchMDBListTMDB(cli *http.Client, listURL, apiKey string) ([]int, error) {
	url := strings.TrimSpace(listURL)
	if !strings.HasPrefix(url, "http") {
		if apiKey == "" {
			apiKey = os.Getenv("MDBLIST_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("mdblist api key required")
		}
		url = "https://mdblist.com/api/" + apiKey + "/list/" + url
	}
	resp, err := cli.Get(url) //nolint:gosec // mdblist URL is operator-configured
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mdblist API %d", resp.StatusCode)
	}
	var items []struct {
		MediaType string `json:"mediatype"`
		ID        int    `json:"id"`
		TMDBID    int    `json:"tmdbid"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		var wrapped struct {
			Movies []struct {
				TMDBID int `json:"tmdbid"`
			} `json:"movies"`
			Shows []struct {
				TMDBID int `json:"tmdbid"`
			} `json:"shows"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			return nil, err
		}
		var ids []int
		for _, mv := range wrapped.Movies {
			if mv.TMDBID > 0 {
				ids = append(ids, mv.TMDBID)
			}
		}
		for _, sh := range wrapped.Shows {
			if sh.TMDBID > 0 {
				ids = append(ids, sh.TMDBID)
			}
		}
		return ids, nil
	}
	var ids []int
	for _, it := range items {
		id := it.TMDBID
		if id == 0 {
			id = it.ID
		}
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func parseTMDBIDList(raw string) []int32 {
	var ids []int
	_ = json.Unmarshal([]byte(raw), &ids)
	out := make([]int32, 0, len(ids))
	for _, id := range ids {
		out = append(out, int32(id)) //nolint:gosec // tmdb ids fit maintainer int32 wire format
	}
	return out
}
