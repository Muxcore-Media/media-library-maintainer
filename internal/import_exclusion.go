package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

func (m *Module) loadImportExclusionSet(ctx context.Context) map[string]struct{} {
	out := make(map[string]struct{})
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return out
	}
	rows, err := db.QueryContext(ctx, `SELECT scope, tmdb_id FROM import_exclusions WHERE tmdb_id > 0`)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scope string
		var tmdb int
		if err := rows.Scan(&scope, &tmdb); err != nil {
			continue
		}
		out[scope+":"+fmt.Sprint(tmdb)] = struct{}{}
	}
	return out
}

func (m *Module) isImportExcluded(scope MediaScope, tmdbID int, excluded map[string]struct{}) bool {
	if tmdbID <= 0 {
		return false
	}
	_, ok := excluded[string(scope)+":"+fmt.Sprint(tmdbID)]
	return ok
}

func (m *Module) addImportExclusion(ctx context.Context, c storedCandidate) {
	if !m.getAddListExclusionOnDelete() {
		return
	}
	ec := parseEvalContext(c.CriteriaJSON)
	if ec.TmdbID <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db != nil {
		_, _ = m.db.ExecContext(ctx, `INSERT INTO import_exclusions (scope, tmdb_id, imdb_id, title, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(scope, tmdb_id) DO UPDATE SET title=excluded.title, imdb_id=excluded.imdb_id`,
			c.Scope, ec.TmdbID, ec.ImdbID, c.Title, nowRFC())
	}
	if err := m.radarrAddExclusion(ctx, ec); err != nil {
		slog.Debug("radarr exclusion failed", "title", c.Title, "error", err)
	}
}

func (m *Module) radarrAddExclusion(ctx context.Context, ec EvalContext) error {
	base := os.Getenv("RADARR_URL")
	key := os.Getenv("RADARR_API_KEY")
	if base == "" || key == "" {
		return nil
	}
	if ec.Scope != ScopeMovie {
		return nil
	}
	payload := map[string]any{
		"tmdbId":     ec.TmdbID,
		"movieTitle": ec.Title,
		"movieYear":  ec.Year,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/v3/exclusions", bytes.NewReader(raw)) //nolint:gosec // radarr base URL is operator-configured
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpCli.Do(req) //nolint:gosec // radarr base URL is operator-configured
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("radarr exclusion %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
