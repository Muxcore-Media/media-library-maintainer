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
	"strings"
	"sync"
)

type plexClient struct {
	cli         *http.Client
	sectionKeys map[string]string
	colKeys     map[string]string
	baseURL     string
	token       string
	machineID   string
	mu          sync.Mutex
}

func (m *Module) plexClient() *plexClient {
	base := m.getPlexBaseURL()
	token := m.getPlexToken()
	if base == "" || token == "" {
		return nil
	}
	return &plexClient{
		baseURL:     strings.TrimRight(base, "/"),
		token:       token,
		cli:         m.httpCli,
		sectionKeys: make(map[string]string),
		colKeys:     make(map[string]string),
	}
}

func (m *Module) getPlexBaseURL() string {
	if v := os.Getenv("PLEX_URL"); v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_PLEX_URL"); v != "" {
		return v
	}
	return ""
}

func (m *Module) getPlexToken() string {
	if v := os.Getenv("PLEX_TOKEN"); v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_PLEX_TOKEN"); v != "" {
		return v
	}
	return ""
}

func (p *plexClient) request(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("X-Plex-Client-Identifier", "muxcore-media-library-maintainer")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := p.cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return raw, resp.StatusCode, err
}

func (p *plexClient) machineIdentifier(ctx context.Context) (string, error) {
	if p.machineID != "" {
		return p.machineID, nil
	}
	raw, code, err := p.request(ctx, http.MethodGet, "/", nil)
	if err != nil || code != http.StatusOK {
		return "", fmt.Errorf("plex root status %d", code)
	}
	var resp struct {
		MediaContainer struct {
			MachineIdentifier string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.MediaContainer.MachineIdentifier == "" {
		return "", fmt.Errorf("plex machine id unavailable")
	}
	p.machineID = resp.MediaContainer.MachineIdentifier
	return p.machineID, nil
}

func (p *plexClient) sectionKey(ctx context.Context, scope MediaScope) (string, error) {
	kind := "movie"
	if scope == ScopeSeries || scope == ScopeSeason {
		kind = "show"
	}
	p.mu.Lock()
	if key, ok := p.sectionKeys[kind]; ok && key != "" {
		p.mu.Unlock()
		return key, nil
	}
	p.mu.Unlock()

	raw, code, err := p.request(ctx, http.MethodGet, "/library/sections", nil)
	if err != nil || code != http.StatusOK {
		return "", fmt.Errorf("plex sections status %d", code)
	}
	var resp struct {
		MediaContainer struct {
			Directory []struct {
				Key   string `json:"key"`
				Type  string `json:"type"`
				Title string `json:"title"`
			} `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return "", fmt.Errorf("parse plex sections")
	}
	envKey := "MAINTAINER_PLEX_MOVIE_SECTION"
	if kind == "show" {
		envKey = "MAINTAINER_PLEX_SHOW_SECTION"
	}
	if v := os.Getenv(envKey); v != "" {
		wantType := "movie"
		if kind == "show" {
			wantType = "show"
		}
		for _, d := range resp.MediaContainer.Directory {
			if d.Type == wantType && (strings.EqualFold(d.Title, v) || d.Key == v) {
				p.mu.Lock()
				p.sectionKeys[kind] = d.Key
				p.mu.Unlock()
				return d.Key, nil
			}
		}
	}
	for _, d := range resp.MediaContainer.Directory {
		if d.Type == kind {
			p.mu.Lock()
			p.sectionKeys[kind] = d.Key
			p.mu.Unlock()
			return d.Key, nil
		}
	}
	return "", fmt.Errorf("plex section for %s not found", kind)
}

func (p *plexClient) findItemRatingKey(ctx context.Context, scope MediaScope, ec EvalContext) (string, error) {
	section, err := p.sectionKey(ctx, scope)
	if err != nil {
		return "", err
	}
	guid := ""
	if ec.TmdbID > 0 {
		guid = fmt.Sprintf("tmdb://%d", ec.TmdbID)
	} else if ec.ImdbID != "" {
		guid = "imdb://" + ec.ImdbID
	}
	if guid == "" {
		return "", fmt.Errorf("no guid for plex lookup")
	}
	q := url.Values{}
	q.Set("guid", guid)
	path := fmt.Sprintf("/library/sections/%s/all?%s", section, q.Encode())
	raw, code, err := p.request(ctx, http.MethodGet, path, nil)
	if err != nil || code != http.StatusOK {
		return "", fmt.Errorf("plex item lookup status %d", code)
	}
	var resp struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey string `json:"ratingKey"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.MediaContainer.Metadata) == 0 {
		return "", fmt.Errorf("plex item not found")
	}
	return resp.MediaContainer.Metadata[0].RatingKey, nil
}

func (p *plexClient) itemURI(ctx context.Context, ratingKey string) (string, error) {
	mid, err := p.machineIdentifier(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s", mid, ratingKey), nil
}

func (p *plexClient) ensureCollection(ctx context.Context, scope MediaScope, name, cachedKey string) (string, error) {
	if cachedKey != "" {
		return cachedKey, nil
	}
	section, err := p.sectionKey(ctx, scope)
	if err != nil {
		return "", err
	}
	cacheName := section + ":" + name
	p.mu.Lock()
	if id, ok := p.colKeys[cacheName]; ok && id != "" {
		p.mu.Unlock()
		return id, nil
	}
	p.mu.Unlock()

	q := url.Values{}
	q.Set("type", "18")
	q.Set("title", name)
	path := fmt.Sprintf("/library/sections/%s/all?%s", section, q.Encode())
	raw, code, err := p.request(ctx, http.MethodGet, path, nil)
	if err == nil && code == http.StatusOK {
		var resp struct {
			MediaContainer struct {
				Metadata []struct {
					RatingKey string `json:"ratingKey"`
				} `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		if json.Unmarshal(raw, &resp) == nil && len(resp.MediaContainer.Metadata) > 0 {
			id := resp.MediaContainer.Metadata[0].RatingKey
			p.mu.Lock()
			p.colKeys[cacheName] = id
			p.mu.Unlock()
			return id, nil
		}
	}
	return "", fmt.Errorf("plex collection %q not found (create via first item add)", name)
}

func (p *plexClient) createCollectionWithItem(ctx context.Context, scope MediaScope, name, itemRatingKey string) (string, error) {
	section, err := p.sectionKey(ctx, scope)
	if err != nil {
		return "", err
	}
	itemURI, err := p.itemURI(ctx, itemRatingKey)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("type", "1")
	q.Set("title", name)
	q.Set("smart", "0")
	q.Set("sectionId", section)
	q.Set("uri", itemURI)
	path := "/library/collections?" + q.Encode()
	raw, code, err := p.request(ctx, http.MethodPost, path, nil)
	if err != nil || code >= 300 {
		return "", fmt.Errorf("plex create collection status %d", code)
	}
	var resp struct {
		Metadata []struct {
			RatingKey string `json:"ratingKey"`
		} `json:"Metadata"`
	}
	if json.Unmarshal(raw, &resp) == nil && len(resp.Metadata) > 0 {
		return resp.Metadata[0].RatingKey, nil
	}
	return p.ensureCollection(ctx, scope, name, "")
}

func (p *plexClient) addToCollection(ctx context.Context, collectionKey, itemRatingKey string) error {
	itemURI, err := p.itemURI(ctx, itemRatingKey)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("uri", itemURI)
	path := fmt.Sprintf("/library/collections/%s/items?%s", url.PathEscape(collectionKey), q.Encode())
	_, code, err := p.request(ctx, http.MethodPut, path, nil)
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("plex add to collection status %d", code)
	}
	return nil
}

func (p *plexClient) removeFromCollection(ctx context.Context, collectionKey, itemRatingKey string) error {
	itemURI, err := p.itemURI(ctx, itemRatingKey)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("uri", itemURI)
	path := fmt.Sprintf("/library/collections/%s/items?%s", url.PathEscape(collectionKey), q.Encode())
	_, code, err := p.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if code >= 300 && code != http.StatusNotFound {
		return fmt.Errorf("plex remove from collection status %d", code)
	}
	return nil
}

func (m *Module) syncLeavingSoonToPlex(ctx context.Context, ec EvalContext, col *storedCollection) {
	if col == nil || !col.LeavingSoonEnabled {
		return
	}
	if ec.Scope != ScopeMovie && ec.Scope != ScopeSeries {
		return
	}
	px := m.plexClient()
	if px == nil {
		return
	}
	label := col.LeavingSoonLabel
	if label == "" {
		label = "Leaving Soon"
	}
	itemKey, err := px.findItemRatingKey(ctx, ec.Scope, ec)
	if err != nil {
		slog.Debug("plex leaving-soon: item lookup failed", "title", ec.Title, "error", err)
		return
	}
	collectionKey := col.PlexCollectionKey
	if collectionKey == "" {
		collectionKey, err = px.createCollectionWithItem(ctx, ec.Scope, label, itemKey)
		if err != nil {
			slog.Debug("plex leaving-soon: create collection", "title", ec.Title, "error", err)
			return
		}
		if collectionKey != "" {
			m.savePlexCollectionKey(ctx, col.ID, collectionKey)
			col.PlexCollectionKey = collectionKey
		}
		return
	}
	if err := px.addToCollection(ctx, collectionKey, itemKey); err != nil {
		slog.Warn("plex leaving-soon: add item", "title", ec.Title, "error", err)
	}
}

func (m *Module) removeLeavingSoonFromPlex(ctx context.Context, ec EvalContext, collectionKey, label string) {
	if collectionKey == "" && label == "" {
		return
	}
	if ec.Scope != ScopeMovie && ec.Scope != ScopeSeries {
		return
	}
	px := m.plexClient()
	if px == nil {
		return
	}
	if label == "" {
		label = "Leaving Soon"
	}
	itemKey, err := px.findItemRatingKey(ctx, ec.Scope, ec)
	if err != nil {
		return
	}
	colKey := collectionKey
	if colKey == "" {
		var err2 error
		colKey, err2 = px.ensureCollection(ctx, ec.Scope, label, "")
		if err2 != nil {
			return
		}
	}
	_ = px.removeFromCollection(ctx, colKey, itemKey)
}

func (m *Module) savePlexCollectionKey(ctx context.Context, collectionID, plexKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx, `UPDATE collections SET plex_collection_key = ? WHERE id = ?`, plexKey, collectionID)
}
