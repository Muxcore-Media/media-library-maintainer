package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ffprobev1 "github.com/Muxcore-Media/media-ffprobe/proto/ffprobev1"
	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
	monitorv1 "github.com/Muxcore-Media/playback-monitor/proto/monitorv1"
	requestmedia "github.com/Muxcore-Media/request-media/proto/requestmedia"
	"github.com/Muxcore-Media/userdata-local/store"

	"github.com/Muxcore-Media/core/sdk/go/client"
)

func (m *Module) dialCore(ctx context.Context) {
	meshAddr := os.Getenv("MUXCORE_GRPC_ADDR")
	if meshAddr == "" {
		meshAddr = "localhost:9090"
	}
	insecureMode := os.Getenv("MUXCORE_INSECURE_DISABLE_TLS") == "true" || os.Getenv("MUXCORE_GRPC_INSECURE") == "true"
	var opts []client.Option
	if insecureMode {
		opts = append(opts, client.WithInsecure())
	}
	c, err := client.Dial(meshAddr, opts...)
	if err != nil {
		return
	}
	m.mu.Lock()
	m.mc = c
	m.mu.Unlock()
}

func (m *Module) findCapabilityAddr(ctx context.Context, capability string) (string, error) {
	m.mu.RLock()
	mc := m.mc
	m.mu.RUnlock()
	if mc == nil {
		return "", fmt.Errorf("not connected to core")
	}
	modules, err := mc.Discovery.FindByCapability(ctx, capability)
	if err != nil {
		return "", err
	}
	for _, mod := range modules {
		addr := dialAddrForModule(mod.Id, mod.HttpAddr)
		if addr != "" {
			return addr, nil
		}
	}
	return "", fmt.Errorf("no %s module found", capability)
}

func dialAddrForModule(moduleID, httpAddr string) string {
	if httpAddr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(httpAddr)
	if err != nil || port == "" {
		return httpAddr
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return net.JoinHostPort(host, port)
	}
	if os.Getenv("MUXCORE_MESH_DIAL_LOCAL") == "true" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	if moduleID != "" {
		return net.JoinHostPort(moduleID, port)
	}
	return httpAddr
}

func (m *Module) ensureMovies(ctx context.Context) error {
	m.mu.RLock()
	if m.moviesClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.library.movies")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.moviesConn = conn
	m.moviesClient = mgmntv1.NewMovieManagementServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureTV(ctx context.Context) error {
	m.mu.RLock()
	if m.tvClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.library.tv")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.tvConn = conn
	m.tvClient = tvmgmtv1.NewTvManagementServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureRequests(ctx context.Context) error {
	m.mu.RLock()
	if m.requestClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.request")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.requestsConn = conn
	m.requestClient = requestmedia.NewRequestServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensurePlaybackMonitor(ctx context.Context) error {
	m.mu.RLock()
	if m.playbackClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "playback.monitor")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.playbackConn = conn
	m.playbackClient = monitorv1.NewPlaybackMonitorServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureFFprobe(ctx context.Context) error {
	m.mu.RLock()
	if m.ffprobeClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.analyzer")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ffprobeConn = conn
	m.ffprobeClient = ffprobev1.NewAnalysisServiceClient(conn)
	m.mu.Unlock()
	return nil
}

type watchStats struct {
	LastWatchedAt          time.Time
	UserDurationMinutes    map[string]float64
	UserWatchedPercent     map[string]float64
	ViewCount              int
	DaysSinceLastWatch     int
	PlayCount              int
	UniqueUsers            int
	TotalDurationMinutes   float64
	LongestDurationMinutes float64
	NeverWatched           bool
	HasActivity            bool
}

func applyWatchStats(ec *EvalContext, ws watchStats) {
	if ec == nil {
		return
	}
	ec.ViewCount = ws.ViewCount
	ec.LastWatchedAt = ws.LastWatchedAt
	ec.NeverWatched = ws.NeverWatched
	ec.DaysSinceLastWatch = ws.DaysSinceLastWatch
	ec.PlaybackPlayCount = ws.PlayCount
	ec.PlaybackUniqueUsers = ws.UniqueUsers
	ec.PlaybackTotalDurationMinutes = ws.TotalDurationMinutes
	ec.PlaybackLongestDurationMinutes = ws.LongestDurationMinutes
	ec.PlaybackHasActivity = ws.HasActivity
	if len(ws.UserDurationMinutes) > 0 {
		ec.UserWatchedDurationMinutes = cloneFloatMap(ws.UserDurationMinutes)
	}
	if len(ws.UserWatchedPercent) > 0 {
		ec.UserWatchedPercent = cloneFloatMap(ws.UserWatchedPercent)
	}
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *Module) watchStatsForItem(ctx context.Context, itemID string, runtimeMinutes int) watchStats {
	if ws, ok := m.watchStatsFromMonitor(ctx, itemID, runtimeMinutes); ok {
		return ws
	}
	return m.watchStatsFromUserdata(itemID, runtimeMinutes)
}

func (m *Module) watchStatsFromMonitor(ctx context.Context, itemID string, runtimeMinutes int) (watchStats, bool) {
	if err := m.ensurePlaybackMonitor(ctx); err != nil {
		return watchStats{}, false
	}
	m.mu.RLock()
	monitorClient := m.playbackClient
	m.mu.RUnlock()
	if monitorClient == nil {
		return watchStats{}, false
	}
	resp, err := monitorClient.GetItemWatchStats(ctx, &monitorv1.GetItemWatchStatsRequest{
		ItemId:         itemID,
		RuntimeMinutes: int32(runtimeMinutes), //nolint:gosec // runtime minutes are bounded media metadata
	})
	if err != nil {
		return watchStats{}, false
	}
	ws := watchStats{
		ViewCount:              int(resp.GetViewCount()),
		PlayCount:              int(resp.GetPlayCount()),
		UniqueUsers:            int(resp.GetUniqueUserCount()),
		TotalDurationMinutes:   resp.GetTotalDurationMinutes(),
		LongestDurationMinutes: resp.GetLongestDurationMinutes(),
		HasActivity:            resp.GetHasActivity(),
		NeverWatched:           resp.GetNeverWatched(),
		DaysSinceLastWatch:     int(resp.GetDaysSinceLastWatch()),
		UserDurationMinutes:    cloneFloatMap(resp.GetUserWatchedDurationMinutes()),
		UserWatchedPercent:     cloneFloatMap(resp.GetUserWatchedPercent()),
	}
	if resp.GetLastWatchedAtUnix() > 0 {
		ws.LastWatchedAt = time.Unix(resp.GetLastWatchedAtUnix(), 0).UTC()
	}
	if ws.UserDurationMinutes == nil {
		ws.UserDurationMinutes = make(map[string]float64)
	}
	if ws.UserWatchedPercent == nil {
		ws.UserWatchedPercent = make(map[string]float64)
	}
	return ws, true
}

func (m *Module) watchStatsFromUserdata(itemID string, runtimeMinutes int) watchStats {
	stats := watchStats{
		NeverWatched:        true,
		DaysSinceLastWatch:  999999,
		UserDurationMinutes: make(map[string]float64),
		UserWatchedPercent:  make(map[string]float64),
	}
	runtimeSec := float64(runtimeMinutes) * 60
	dir := m.userdataDir()
	if dir == "" {
		return stats
	}
	for _, path := range listJSONFiles(dir) {
		b, err := os.ReadFile(path) //nolint:gosec // userdata paths are resolved from local store layout
		if err != nil {
			continue
		}
		var blob store.Blob
		if err := json.Unmarshal(b, &blob); err != nil {
			continue
		}
		raw, ok := blob.Progress[itemID]
		if !ok {
			continue
		}
		var pe progressEntry
		if err := json.Unmarshal(raw, &pe); err != nil {
			continue
		}
		user := normalizePlaybackUser(blob.UserID)
		if user == "" {
			user = normalizePlaybackUser(strings.TrimSuffix(filepath.Base(path), ".json"))
		}
		durationMin := pe.PositionSec / 60.0
		if user != "" {
			stats.UserDurationMinutes[user] += durationMin
			percent := watchedPercent(pe, runtimeSec)
			if prev, ok := stats.UserWatchedPercent[user]; !ok || percent > prev {
				stats.UserWatchedPercent[user] = percent
			}
		}
		stats.NeverWatched = false
		stats.HasActivity = true
		stats.UniqueUsers++
		stats.PlayCount++
		stats.ViewCount++
		stats.TotalDurationMinutes += pe.PositionSec / 60.0
		posMin := pe.PositionSec / 60.0
		if posMin > stats.LongestDurationMinutes {
			stats.LongestDurationMinutes = posMin
		}
		if t, err := time.Parse(time.RFC3339, pe.UpdatedAt); err == nil {
			if stats.LastWatchedAt.IsZero() || t.After(stats.LastWatchedAt) {
				stats.LastWatchedAt = t
			}
		}
		if pe.Watched {
			stats.ViewCount++
			stats.PlayCount++
		}
	}
	if !stats.LastWatchedAt.IsZero() {
		stats.DaysSinceLastWatch = daysSince(stats.LastWatchedAt)
		stats.NeverWatched = false
	}
	if stats.PlayCount == 0 && stats.ViewCount > 0 {
		stats.PlayCount = stats.ViewCount
	}
	return stats
}

func watchedPercent(pe progressEntry, runtimeSec float64) float64 {
	if pe.Watched {
		return 100
	}
	if runtimeSec <= 0 || pe.PositionSec <= 0 {
		return 0
	}
	pct := pe.PositionSec / runtimeSec * 100
	if pct > 100 {
		return 100
	}
	return pct
}

type progressEntry struct {
	ID          string  `json:"id"`
	UpdatedAt   string  `json:"updatedAt"`
	PositionSec float64 `json:"positionSec"`
	Watched     bool    `json:"watched"`
}

func listJSONFiles(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".json") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func (m *Module) userdataDir() string {
	m.cfgMu.RLock()
	dir := m.userdataDataDir
	m.cfgMu.RUnlock()
	if dir != "" {
		return dir
	}
	if v := os.Getenv("MAINTAINER_USERDATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("USERDATA_LOCAL_DATA_DIR"); v != "" {
		return v
	}
	return "/var/lib/muxcore-userdata"
}

func (m *Module) loadRequestIndex(ctx context.Context) map[string]requestInfo {
	out := make(map[string]requestInfo)
	if err := m.ensureRequests(ctx); err != nil {
		return out
	}
	m.mu.RLock()
	rc := m.requestClient
	m.mu.RUnlock()
	if rc == nil {
		return out
	}
	resp, err := rc.ListRequests(ctx, &requestmedia.ListRequestsRequest{})
	if err != nil {
		return out
	}
	for _, r := range resp.GetRequests() {
		key := r.GetItemType() + ":" + r.GetItemId()
		if key == ":" {
			key = fmt.Sprintf("%s:%d", r.GetItemType(), r.GetTmdbId())
		}
		created, _ := time.Parse(time.RFC3339, r.GetCreatedAt())
		out[key] = requestInfo{
			Requested:   true,
			RequestedBy: r.GetRequestedBy(),
			CreatedAt:   created,
		}
	}
	return out
}

type requestInfo struct {
	CreatedAt   time.Time
	RequestedBy string
	Requested   bool
}

func parseTimeRFC(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
