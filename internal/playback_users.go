package internal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
	monitorv1 "github.com/Muxcore-Media/playback-monitor/proto/monitorv1"
	"github.com/Muxcore-Media/userdata-local/store"
)

func (m *Module) ListPlaybackUsers(ctx context.Context, req *maintainv1.ListPlaybackUsersRequest) (*maintainv1.ListPlaybackUsersResponse, error) {
	users := m.listPlaybackUsers(ctx, req.GetQuery(), int(req.GetLimit()))
	out := make([]*maintainv1.PlaybackUser, 0, len(users))
	for _, u := range users {
		out = append(out, &maintainv1.PlaybackUser{Username: u})
	}
	return &maintainv1.ListPlaybackUsersResponse{Users: out}, nil
}

func (m *Module) listPlaybackUsers(ctx context.Context, query string, limit int) []string {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if users, ok := m.listPlaybackUsersFromMonitor(ctx, query, limit); ok {
		return users
	}
	return m.listPlaybackUsersFromUserdata(query, limit)
}

func (m *Module) listPlaybackUsersFromMonitor(ctx context.Context, query string, limit int) ([]string, bool) {
	if err := m.ensurePlaybackMonitor(ctx); err != nil {
		return nil, false
	}
	m.mu.RLock()
	client := m.playbackClient
	m.mu.RUnlock()
	if client == nil {
		return nil, false
	}
	resp, err := client.ListWatchUsers(ctx, &monitorv1.ListWatchUsersRequest{
		Query: query,
		Limit: int32(limit), //nolint:gosec // list limits are bounded to 500 in caller
	})
	if err != nil {
		return nil, false
	}
	out := make([]string, 0, len(resp.GetUsers()))
	for _, u := range resp.GetUsers() {
		if name := strings.TrimSpace(u.GetUsername()); name != "" {
			out = append(out, name)
		}
	}
	return out, true
}

func (m *Module) listPlaybackUsersFromUserdata(query string, limit int) []string {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	dir := m.userdataDir()
	if dir == "" {
		return nil
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]string)
	for _, path := range listJSONFiles(dir) {
		user := playbackUserFromPath(path)
		b, err := os.ReadFile(path) //nolint:gosec // userdata paths are resolved from local store layout
		if err == nil {
			var blob store.Blob
			if json.Unmarshal(b, &blob) == nil && strings.TrimSpace(blob.UserID) != "" {
				user = strings.TrimSpace(blob.UserID)
			}
		}
		user = strings.TrimSpace(user)
		if user == "" {
			continue
		}
		key := strings.ToLower(user)
		if needle != "" && !strings.Contains(key, needle) {
			continue
		}
		if _, ok := seen[key]; !ok {
			seen[key] = user
		}
	}
	out := make([]string, 0, len(seen))
	for _, user := range seen {
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func playbackUserFromPath(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".json") {
		return ""
	}
	return strings.TrimSuffix(base, ".json")
}
