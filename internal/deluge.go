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

type delugeClient struct {
	baseURL  string
	password string
	cli      *http.Client
}

func (m *Module) getDelugeURL() string {
	if v := os.Getenv("DELUGE_URL"); v != "" {
		return v
	}
	return os.Getenv("MAINTAINER_DELUGE_URL")
}

func (m *Module) getDelugePassword() string {
	if v := os.Getenv("DELUGE_PASSWORD"); v != "" {
		return v
	}
	return os.Getenv("MAINTAINER_DELUGE_PASSWORD")
}

func (m *Module) delugeClient() *delugeClient {
	url := m.getDelugeURL()
	if url == "" {
		return nil
	}
	return &delugeClient{
		baseURL:  strings.TrimRight(url, "/") + "/json",
		password: m.getDelugePassword(),
		cli:      m.httpCli,
	}
}

func (d *delugeClient) rpc(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	payload := map[string]any{
		"method": method,
		"params": params,
		"id":     1,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil, fmt.Errorf("parse deluge response")
	}
	if out.Error != nil {
		return nil, fmt.Errorf("deluge: %s", out.Error.Message)
	}
	return out.Result, nil
}

func (d *delugeClient) login(ctx context.Context) error {
	res, err := d.rpc(ctx, "auth.login", []any{d.password})
	if err != nil {
		return err
	}
	var ok bool
	if json.Unmarshal(res, &ok) != nil || !ok {
		return fmt.Errorf("deluge login failed")
	}
	return nil
}

func (d *delugeClient) removeDownloads(ctx context.Context, downloadIDs []string, deleteData bool, fallbackRatio float64) error {
	if err := d.login(ctx); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, id := range downloadIDs {
		hash := strings.ToLower(strings.TrimSpace(id))
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		name, ratio, done, ok := d.torrentByHash(ctx, hash)
		if !ok {
			continue
		}
		if !done && ratio < fallbackRatio {
			slog.Debug("keeping deluge torrent seeding", "name", name, "ratio", ratio)
			continue
		}
		if _, err := d.rpc(ctx, "core.remove_torrent", []any{hash, deleteData}); err != nil {
			slog.Warn("failed to remove deluge torrent", "hash", hash, "error", err)
			continue
		}
		slog.Info("removed torrent from deluge", "name", name, "deleteData", deleteData)
	}
	return nil
}

func (d *delugeClient) torrentByHash(ctx context.Context, hash string) (name string, ratio float64, done bool, ok bool) {
	res, err := d.rpc(ctx, "core.get_torrents_status", []any{map[string]any{"hash": hash}, []string{"name", "ratio", "is_seed", "seeding_time", "total_done", "total_size"}})
	if err != nil {
		return "", 0, false, false
	}
	var status map[string]struct {
		Name      string  `json:"name"`
		Ratio     float64 `json:"ratio"`
		IsSeed    bool    `json:"is_seed"`
		TotalDone int64   `json:"total_done"`
		TotalSize int64   `json:"total_size"`
	}
	if json.Unmarshal(res, &status) != nil {
		return "", 0, false, false
	}
	for _, tor := range status {
		done = tor.IsSeed || (tor.TotalSize > 0 && tor.TotalDone >= tor.TotalSize)
		return tor.Name, tor.Ratio, done, true
	}
	return "", 0, false, false
}
