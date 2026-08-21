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

type qbittorrentClient struct {
	baseURL string
	user    string
	pass    string
	cli     *http.Client
	mu      sync.Mutex
	cookie  string
	auth    bool
}

type qbittorrentTorrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	ContentPath  string  `json:"content_path"`
	Ratio        float64 `json:"ratio"`
	MaxRatio     float64 `json:"max_ratio"`
	SeedingTime  int64   `json:"seeding_time"`
	MaxSeedingTime int64 `json:"max_seeding_time"`
}

func (m *Module) qbitClient() *qbittorrentClient {
	url := m.getDownloadClientURL()
	if url == "" {
		return nil
	}
	return &qbittorrentClient{
		baseURL: strings.TrimRight(url, "/"),
		user:    m.getDownloadClientUsername(),
		pass:    m.getDownloadClientPassword(),
		cli:     m.httpCli,
	}
}

func (m *Module) getDownloadClientURL() string {
	m.cfgMu.RLock()
	v := m.downloadClientURL
	m.cfgMu.RUnlock()
	if v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_QBITTORRENT_URL"); v != "" {
		return v
	}
	return os.Getenv("QBITTORRENT_URL")
}

func (m *Module) getDownloadClientUsername() string {
	m.cfgMu.RLock()
	v := m.downloadClientUser
	m.cfgMu.RUnlock()
	if v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_QBITTORRENT_USERNAME"); v != "" {
		return v
	}
	return os.Getenv("QBITTORRENT_USERNAME")
}

func (m *Module) getDownloadClientPassword() string {
	m.cfgMu.RLock()
	v := m.downloadClientPass
	m.cfgMu.RUnlock()
	if v != "" {
		return v
	}
	if v := os.Getenv("MAINTAINER_QBITTORRENT_PASSWORD"); v != "" {
		return v
	}
	return os.Getenv("QBITTORRENT_PASSWORD")
}

func (m *Module) getDownloadClientEnabled() bool {
	return m.getDownloadClientURL() != "" || m.getTransmissionURL() != "" || m.getDelugeURL() != ""
}

func (m *Module) getDownloadClientDeleteData() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.downloadClientDeleteDataSet {
		return m.downloadClientDeleteData
	}
	return !strings.EqualFold(os.Getenv("MAINTAINER_QBITTORRENT_KEEP_DATA"), "true")
}

func (m *Module) getDownloadClientFallbackRatio() float64 {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.downloadClientFallbackRatio > 0 {
		return m.downloadClientFallbackRatio
	}
	return 0.5
}

func (q *qbittorrentClient) apiURL(path string) string {
	return q.baseURL + "/api/v2" + path
}

func (q *qbittorrentClient) login(ctx context.Context) error {
	body := url.Values{}
	body.Set("username", q.user)
	body.Set("password", q.pass)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.apiURL("/auth/login"), strings.NewReader(body.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", q.baseURL)
	resp, err := q.cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized || strings.TrimSpace(string(raw)) == "Fails." {
		return fmt.Errorf("qbittorrent login failed")
	}
	if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
		q.cookie = strings.Join(cookies, "; ")
	}
	q.auth = true
	return nil
}

func (q *qbittorrentClient) do(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, int, error) {
	q.mu.Lock()
	if !q.auth {
		if err := q.login(ctx); err != nil {
			q.mu.Unlock()
			return nil, 0, err
		}
	}
	cookie := q.cookie
	q.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, method, q.apiURL(path), body)
	if err != nil {
		return nil, 0, err
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	req.Header.Set("Referer", q.baseURL)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := q.cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && err == nil {
		q.mu.Lock()
		q.auth = false
		q.cookie = ""
		q.mu.Unlock()
		if loginErr := q.login(ctx); loginErr == nil {
			return q.do(ctx, method, path, body, contentType)
		}
	}
	return raw, resp.StatusCode, err
}

func (q *qbittorrentClient) getTorrent(ctx context.Context, hash string) (*qbittorrentTorrent, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	raw, code, err := q.do(ctx, http.MethodGet, "/torrents/info?hashes="+url.QueryEscape(hash), nil, "")
	if err != nil || code != http.StatusOK {
		return nil, fmt.Errorf("torrent info status %d", code)
	}
	var list []qbittorrentTorrent
	if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

func (q *qbittorrentClient) listTorrents(ctx context.Context) ([]qbittorrentTorrent, error) {
	raw, code, err := q.do(ctx, http.MethodGet, "/torrents/info", nil, "")
	if err != nil || code != http.StatusOK {
		return nil, fmt.Errorf("torrent list status %d", code)
	}
	var list []qbittorrentTorrent
	if json.Unmarshal(raw, &list) != nil {
		return nil, fmt.Errorf("parse torrent list")
	}
	return list, nil
}

func toClientTorrent(raw qbittorrentTorrent) qbittorrentTorrent {
	if raw.Ratio == -1 {
		raw.Ratio = 1e18
	}
	return raw
}

func shouldRemoveTorrent(t qbittorrentTorrent, fallbackRatio float64) bool {
	hasRatioLimit := t.MaxRatio >= 0
	hasTimeLimit := t.MaxSeedingTime >= 0
	if !hasRatioLimit && !hasTimeLimit {
		return t.Ratio >= fallbackRatio
	}
	ratioMet := hasRatioLimit && t.Ratio >= t.MaxRatio
	timeMet := hasTimeLimit && t.SeedingTime >= t.MaxSeedingTime
	return ratioMet || timeMet
}

func (q *qbittorrentClient) removeDownloads(ctx context.Context, downloadIDs []string, deleteData bool, fallbackRatio float64) error {
	pathCounts := map[string]int{}
	if deleteData {
		if list, err := q.listTorrents(ctx); err == nil {
			for _, t := range list {
				p := strings.TrimSpace(t.ContentPath)
				if p == "" {
					continue
				}
				pathCounts[p]++
			}
		}
	}
	seen := map[string]struct{}{}
	for _, id := range downloadIDs {
		hash := strings.ToLower(strings.TrimSpace(id))
		if hash == "" || hash == "all" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		raw, err := q.getTorrent(ctx, hash)
		if err != nil || raw == nil {
			continue
		}
		t := toClientTorrent(*raw)
		if !shouldRemoveTorrent(t, fallbackRatio) {
			slog.Debug("keeping torrent seeding", "name", t.Name, "ratio", t.Ratio)
			continue
		}
		deleteDataForThis := deleteData
		if deleteData && strings.TrimSpace(t.ContentPath) != "" && pathCounts[t.ContentPath] > 1 {
			deleteDataForThis = false
		}
		if err := q.deleteTorrents(ctx, []string{hash}, deleteDataForThis); err != nil {
			slog.Warn("failed to remove torrent", "hash", hash, "error", err)
		} else {
			slog.Info("removed torrent from download client", "name", t.Name, "deleteData", deleteDataForThis)
		}
	}
	return nil
}

func (q *qbittorrentClient) deleteTorrents(ctx context.Context, hashes []string, deleteFiles bool) error {
	body := url.Values{}
	body.Set("hashes", strings.Join(hashes, "|"))
	body.Set("deleteFiles", strconvBool(deleteFiles))
	_, code, err := q.do(ctx, http.MethodPost, "/torrents/delete", strings.NewReader(body.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("delete torrents status %d", code)
	}
	return nil
}

func strconvBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
