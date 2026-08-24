package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Muxcore-Media/userdata-local/store"
)

func (m *Module) shouldProtectUnwatchedRequester(ec EvalContext) bool {
	if !m.getProtectUnwatchedRequestersEnabled() {
		return false
	}
	if !ec.Requested || ec.RequestedBy == "" {
		return false
	}
	minAge := m.getProtectRequestMinAgeDays()
	if ec.DaysSinceRequest < minAge {
		return true
	}
	maxDays := m.getProtectRequestMaxDays()
	if maxDays > 0 && ec.DaysSinceRequest > maxDays {
		return false
	}
	return !ec.RequesterWatched
}

func (m *Module) requesterHasWatched(itemID, requester string) bool {
	userID := m.mapRequesterToUserID(requester)
	if userID == "" {
		return false
	}
	dir := m.userdataDir()
	for _, path := range m.userdataPathsForUser(dir, userID) {
		b, err := os.ReadFile(path) //nolint:gosec // userdata paths are resolved from local store layout
		if err != nil {
			continue
		}
		var blob store.Blob
		if json.Unmarshal(b, &blob) != nil {
			continue
		}
		raw, ok := blob.Progress[itemID]
		if !ok {
			continue
		}
		var pe progressEntry
		if json.Unmarshal(raw, &pe) != nil {
			continue
		}
		if pe.Watched {
			return true
		}
	}
	return false
}

func (m *Module) mapRequesterToUserID(requester string) string {
	requester = strings.TrimSpace(requester)
	if requester == "" {
		return ""
	}
	m.cfgMu.RLock()
	if mapped, ok := m.requestUserMap[requester]; ok && mapped != "" {
		m.cfgMu.RUnlock()
		return mapped
	}
	m.cfgMu.RUnlock()
	if raw := os.Getenv("MAINTAINER_REQUEST_USER_MAP"); raw != "" {
		var mapping map[string]string
		if json.Unmarshal([]byte(raw), &mapping) == nil {
			if mapped, ok := mapping[requester]; ok && mapped != "" {
				return mapped
			}
		}
	}
	return requester
}

func (m *Module) userdataPathsForUser(root, userID string) []string {
	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(userID)
	var out []string
	candidates := []string{
		filepath.Join(root, safe+".json"),
		filepath.Join(root, "tenants", "default", safe+".json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return walkErr
		}
		if strings.HasSuffix(path, safe+".json") {
			out = append(out, path)
		}
		return nil
	})
	if len(out) == 0 {
		return candidates
	}
	return out
}
