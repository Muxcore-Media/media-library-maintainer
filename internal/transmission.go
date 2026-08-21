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

type torrentCleanupClient interface {
	removeDownloads(ctx context.Context, downloadIDs []string, deleteData bool, fallbackRatio float64) error
}

func (m *Module) torrentCleanupClient() torrentCleanupClient {
	if m.getDownloadClientURL() != "" {
		return m.qbitClient()
	}
	if m.getTransmissionURL() != "" {
		return m.transmissionClient()
	}
	if m.getDelugeURL() != "" {
		return m.delugeClient()
	}
	return nil
}

func (m *Module) getTransmissionURL() string {
	if v := os.Getenv("TRANSMISSION_URL"); v != "" {
		return v
	}
	return os.Getenv("MAINTAINER_TRANSMISSION_URL")
}

func (m *Module) getTransmissionUsername() string {
	if v := os.Getenv("TRANSMISSION_USERNAME"); v != "" {
		return v
	}
	return os.Getenv("MAINTAINER_TRANSMISSION_USERNAME")
}

func (m *Module) getTransmissionPassword() string {
	if v := os.Getenv("TRANSMISSION_PASSWORD"); v != "" {
		return v
	}
	return os.Getenv("MAINTAINER_TRANSMISSION_PASSWORD")
}

type transmissionClient struct {
	baseURL  string
	user     string
	pass     string
	cli      *http.Client
	sessionID string
}

func (m *Module) transmissionClient() *transmissionClient {
	url := m.getTransmissionURL()
	if url == "" {
		return nil
	}
	return &transmissionClient{
		baseURL: strings.TrimRight(url, "/"),
		user:    m.getTransmissionUsername(),
		pass:    m.getTransmissionPassword(),
		cli:     m.httpCli,
	}
}

func (t *transmissionClient) rpc(ctx context.Context, method string, args map[string]any) (json.RawMessage, error) {
	payload := map[string]any{
		"method":    method,
		"arguments": args,
	}
	if t.sessionID != "" {
		payload["tag"] = t.sessionID
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/transmission/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if t.user != "" || t.pass != "" {
		req.SetBasicAuth(t.user, t.pass)
	}
	if t.sessionID != "" {
		req.Header.Set("X-Transmission-Session-Id", t.sessionID)
	}
	resp, err := t.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		t.sessionID = resp.Header.Get("X-Transmission-Session-Id")
		return t.rpc(ctx, method, args)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("transmission status %d", resp.StatusCode)
	}
	var out struct {
		Result    string          `json:"result"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil, fmt.Errorf("parse transmission response")
	}
	if out.Result != "success" {
		return nil, fmt.Errorf("transmission %s", out.Result)
	}
	return out.Arguments, nil
}

func (t *transmissionClient) torrentByHash(ctx context.Context, hash string) (name, downloadDir string, ratio float64, done bool, ok bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	args, err := t.rpc(ctx, "torrent-get", map[string]any{
		"ids":    []string{hash},
		"fields": []string{"hashString", "name", "downloadDir", "uploadRatio", "isFinished", "seedRatioMode", "seedRatioLimit"},
	})
	if err != nil {
		return "", "", 0, false, false
	}
	var resp struct {
		Torrents []struct {
			HashString    string  `json:"hashString"`
			Name          string  `json:"name"`
			DownloadDir   string  `json:"downloadDir"`
			UploadRatio   float64 `json:"uploadRatio"`
			IsFinished    bool    `json:"isFinished"`
			SeedRatioMode int     `json:"seedRatioMode"`
			SeedRatioLimit float64 `json:"seedRatioLimit"`
		} `json:"torrents"`
	}
	if json.Unmarshal(args, &resp) != nil {
		return "", "", 0, false, false
	}
	for _, tor := range resp.Torrents {
		if strings.ToLower(tor.HashString) != hash {
			continue
		}
		done = tor.IsFinished
		if tor.SeedRatioMode == 1 && tor.SeedRatioLimit > 0 {
			done = tor.UploadRatio >= tor.SeedRatioLimit
		}
		return tor.Name, tor.DownloadDir, tor.UploadRatio, done, true
	}
	return "", "", 0, false, false
}

func (t *transmissionClient) removeDownloads(ctx context.Context, downloadIDs []string, deleteData bool, fallbackRatio float64) error {
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
		name, _, ratio, done, ok := t.torrentByHash(ctx, hash)
		if !ok {
			continue
		}
		if !done && ratio < fallbackRatio {
			slog.Debug("keeping transmission torrent seeding", "name", name, "ratio", ratio)
			continue
		}
		_, err := t.rpc(ctx, "torrent-remove", map[string]any{
			"ids":             []string{hash},
			"delete-local-data": deleteData,
		})
		if err != nil {
			slog.Warn("failed to remove transmission torrent", "hash", hash, "error", err)
			continue
		}
		slog.Info("removed torrent from transmission", "name", name, "deleteData", deleteData)
	}
	return nil
}
