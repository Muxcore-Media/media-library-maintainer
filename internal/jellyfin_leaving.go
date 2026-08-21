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

type jellyfinClient struct {
	baseURL string
	apiKey  string
	cli     *http.Client
	mu      sync.Mutex
	colIDs  map[string]string // collection name -> jellyfin id
}

func (m *Module) jellyfinClient() *jellyfinClient {
	base := m.getJellyfinBaseURL()
	key := m.getJellyfinAPIKey()
	if base == "" || key == "" {
		return nil
	}
	return &jellyfinClient{
		baseURL: strings.TrimRight(base, "/"),
		apiKey:  key,
		cli:     m.httpCli,
		colIDs:  make(map[string]string),
	}
}

func (m *Module) getJellyfinBaseURL() string {
	if v := os.Getenv("JELLYFIN_BASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_JELLYFIN_URL"); v != "" {
		return v
	}
	return ""
}

func (m *Module) getJellyfinAPIKey() string {
	if v := os.Getenv("JELLYFIN_API_KEY"); v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_JELLYFIN_API_KEY"); v != "" {
		return v
	}
	return ""
}

func (jf *jellyfinClient) request(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, jf.baseURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, jf.apiKey))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := jf.cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return raw, resp.StatusCode, err
}

func (jf *jellyfinClient) findItemID(ctx context.Context, ec EvalContext) (string, error) {
	itemType := "Movie"
	if ec.Scope == ScopeSeries || ec.Scope == ScopeSeason {
		itemType = "Series"
	}
	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", itemType)
	if ec.TmdbID > 0 {
		q.Set("AnyProviderIdEquals", fmt.Sprintf("Tmdb.%d", ec.TmdbID))
	} else if ec.ImdbID != "" {
		q.Set("AnyProviderIdEquals", "Imdb."+ec.ImdbID)
	} else {
		return "", fmt.Errorf("no provider id for jellyfin lookup")
	}
	raw, code, err := jf.request(ctx, http.MethodGet, "/Items?"+q.Encode(), nil)
	if err != nil || code != http.StatusOK {
		return "", fmt.Errorf("jellyfin lookup failed")
	}
	var resp struct {
		Items []struct {
			ID string `json:"Id"`
		} `json:"Items"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Items) == 0 {
		return "", fmt.Errorf("jellyfin item not found")
	}
	return resp.Items[0].ID, nil
}

func (jf *jellyfinClient) ensureCollection(ctx context.Context, name, cachedID string) (string, error) {
	if cachedID != "" {
		return cachedID, nil
	}
	jf.mu.Lock()
	if id, ok := jf.colIDs[name]; ok && id != "" {
		jf.mu.Unlock()
		return id, nil
	}
	jf.mu.Unlock()

	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", "BoxSet")
	q.Set("Name", name)
	raw, code, err := jf.request(ctx, http.MethodGet, "/Items?"+q.Encode(), nil)
	if err == nil && code == http.StatusOK {
		var resp struct {
			Items []struct {
				ID string `json:"Id"`
			} `json:"Items"`
		}
		if json.Unmarshal(raw, &resp) == nil && len(resp.Items) > 0 {
			id := resp.Items[0].ID
			jf.mu.Lock()
			jf.colIDs[name] = id
			jf.mu.Unlock()
			return id, nil
		}
	}

	createPath := "/Collections?Name=" + url.QueryEscape(name)
	_, code, err = jf.request(ctx, http.MethodPost, createPath, nil)
	if err != nil || code >= 300 {
		return "", fmt.Errorf("create jellyfin collection: status %d", code)
	}
	raw, code, err = jf.request(ctx, http.MethodGet, "/Items?"+q.Encode(), nil)
	if err != nil || code != http.StatusOK {
		return "", fmt.Errorf("reload jellyfin collection")
	}
	var resp struct {
		Items []struct {
			ID string `json:"Id"`
		} `json:"Items"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Items) == 0 {
		return "", fmt.Errorf("collection not found after create")
	}
	id := resp.Items[0].ID
	jf.mu.Lock()
	jf.colIDs[name] = id
	jf.mu.Unlock()
	return id, nil
}

func (jf *jellyfinClient) addToCollection(ctx context.Context, collectionID, itemID string) error {
	path := fmt.Sprintf("/Collections/%s/Items?ids=%s", url.PathEscape(collectionID), url.QueryEscape(itemID))
	_, code, err := jf.request(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("jellyfin add to collection status %d", code)
	}
	return nil
}

func (jf *jellyfinClient) removeFromCollection(ctx context.Context, collectionID, itemID string) error {
	path := fmt.Sprintf("/Collections/%s/Items?ids=%s", url.PathEscape(collectionID), url.QueryEscape(itemID))
	_, code, err := jf.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if code >= 300 && code != http.StatusNotFound {
		return fmt.Errorf("jellyfin remove from collection status %d", code)
	}
	return nil
}

func (m *Module) syncLeavingSoonToJellyfin(ctx context.Context, ec EvalContext, col *storedCollection) {
	if col == nil || !col.LeavingSoonEnabled {
		return
	}
	jf := m.jellyfinClient()
	if jf == nil {
		return
	}
	label := col.LeavingSoonLabel
	if label == "" {
		label = "Leaving Soon"
	}
	itemID, err := jf.findItemID(ctx, ec)
	if err != nil {
		slog.Debug("jellyfin leaving-soon: item lookup failed", "title", ec.Title, "error", err)
		return
	}
	collectionID, err := jf.ensureCollection(ctx, label, col.JellyfinCollectionID)
	if err != nil {
		slog.Warn("jellyfin leaving-soon: ensure collection", "error", err)
		return
	}
	if col.JellyfinCollectionID == "" && collectionID != "" {
		m.saveJellyfinCollectionID(col.ID, collectionID)
		col.JellyfinCollectionID = collectionID
	}
	if err := jf.addToCollection(ctx, collectionID, itemID); err != nil {
		slog.Warn("jellyfin leaving-soon: add item", "title", ec.Title, "error", err)
	}
}

func (m *Module) removeLeavingSoonFromJellyfin(ctx context.Context, ec EvalContext, collectionID, label string) {
	if collectionID == "" && label == "" {
		return
	}
	jf := m.jellyfinClient()
	if jf == nil {
		return
	}
	if label == "" {
		label = "Leaving Soon"
	}
	itemID, err := jf.findItemID(ctx, ec)
	if err != nil {
		return
	}
	colID := collectionID
	if colID == "" {
		var err2 error
		colID, err2 = jf.ensureCollection(ctx, label, "")
		if err2 != nil {
			return
		}
	}
	_ = jf.removeFromCollection(ctx, colID, itemID)
}

func (m *Module) saveJellyfinCollectionID(collectionID, jellyfinID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.Exec(`UPDATE collections SET jellyfin_collection_id = ? WHERE id = ?`, jellyfinID, collectionID)
}
