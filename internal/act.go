package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
	requestmedia "github.com/Muxcore-Media/request-media/proto/requestmedia"
)

type storedCandidate struct {
	ID               string
	Scope            MediaScope
	ItemID           string
	Title            string
	ArrAction        ArrAction
	Status           CandidateStatus
	CollectionID     string
	ActAfter         string
	PostponedUntil   string
	CriteriaJSON     string
	QualityProfileID string
	MatchedRuleIDs   string
	SizeBytes        int64
}

type actOptions struct {
	maxActions        int
	targetFreePercent float64
	dryRun            bool
	freeUp            bool
}

func (m *Module) runAct(ctx context.Context, opts actOptions) (taken, failed int, err error) {
	if opts.maxActions <= 0 {
		opts.maxActions = m.getMaxActionsPerRun()
	}
	var cands []storedCandidate
	if opts.freeUp {
		cands = m.loadFreeUpCandidates(ctx, opts.maxActions)
	} else {
		cands = m.loadActionableCandidates(ctx, opts.maxActions)
	}
	freeUpRoot := m.getFreeUpRootPath()
	for _, c := range cands {
		ec := parseEvalContext(c.CriteriaJSON)
		root := ec.RootFolderPath
		if root == "" {
			root = freeUpRoot
		}
		if !opts.freeUp && !m.diskGateAllowsAction(root) {
			continue
		}
		if opts.freeUp && m.freeUpTargetMet(freeUpRoot, opts.targetFreePercent) {
			break
		}
		profileID := c.QualityProfileID
		if profileID == "" {
			profileID = ec.QualityProfile
		}
		c.QualityProfileID = profileID
		if opts.dryRun {
			taken++
			continue
		}
		actErr := m.executeAction(ctx, c)
		if actErr != nil {
			failed++
			m.markCandidateFailed(ctx, c.ID, actErr.Error())
			slog.Warn("maintainer action failed", "item", c.ItemID, "error", actErr)
			continue
		}
		taken++
		m.closeRequestsForItem(ctx, c)
		m.cleanupDownloadClient(ctx, c)
		m.markCandidateCompleted(ctx, c)
	}
	return taken, failed, nil
}

func parseEvalContext(raw string) EvalContext {
	var ec EvalContext
	_ = json.Unmarshal([]byte(raw), &ec)
	return ec
}

func (m *Module) loadActionableCandidates(ctx context.Context, limit int) []storedCandidate {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return nil
	}
	now := nowRFC()
	rows, err := db.QueryContext(ctx, `SELECT id, scope, item_id, title, arr_action, status, collection_id, act_after, postponed_until, size_bytes, criteria_json, matched_rule_ids
		FROM candidates
		WHERE status IN ('approved','leaving_soon','pending','postponed')
		  AND (postponed_until = '' OR postponed_until <= ?)
		  AND (act_after = '' OR act_after <= ?)
		ORDER BY act_after ASC LIMIT ?`, now, now, limit)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	return m.filterActionableCandidates(ctx, rows, false)
}

func (m *Module) loadFreeUpCandidates(ctx context.Context, limit int) []storedCandidate {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, scope, item_id, title, arr_action, status, collection_id, act_after, postponed_until, size_bytes, criteria_json, matched_rule_ids
		FROM candidates
		WHERE status IN ('approved','leaving_soon','pending','postponed')
		ORDER BY size_bytes DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	return m.filterActionableCandidates(ctx, rows, true)
}

func (m *Module) filterActionableCandidates(ctx context.Context, rows sqlRows, freeUp bool) []storedCandidate {
	defer func() { _ = rows.Close() }()
	var scanned []storedCandidate
	for rows.Next() {
		var c storedCandidate
		if err := rows.Scan(&c.ID, &c.Scope, &c.ItemID, &c.Title, &c.ArrAction, &c.Status, &c.CollectionID, &c.ActAfter, &c.PostponedUntil, &c.SizeBytes, &c.CriteriaJSON, &c.MatchedRuleIDs); err != nil {
			continue
		}
		scanned = append(scanned, c)
	}
	_ = rows.Close()

	var out []storedCandidate
	for _, c := range scanned {
		if c.Status == StatusPostponed {
			now := nowRFC()
			if c.PostponedUntil != "" && c.PostponedUntil > now {
				continue
			}
		}
		if c.Status == StatusPending || c.Status == StatusPostponed {
			if col := m.loadCollectionByID(ctx, c.CollectionID); col != nil && !col.Enabled {
				continue
			}
			if !m.candidateAutoActAllowed(ctx, c) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func (m *Module) candidateAutoActAllowed(ctx context.Context, c storedCandidate) bool {
	if c.Status == StatusApproved || c.Status == StatusLeavingSoon {
		return true
	}
	if c.Status == StatusPostponed {
		now := nowRFC()
		if c.PostponedUntil != "" && c.PostponedUntil <= now {
			return true
		}
	}
	if m.getAutoActEnabled() {
		return true
	}
	var ruleIDs []string
	if err := json.Unmarshal([]byte(c.MatchedRuleIDs), &ruleIDs); err != nil || len(ruleIDs) == 0 {
		return false
	}
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return false
	}
	for _, rid := range ruleIDs {
		var autoAct int
		if err := db.QueryRowContext(ctx, `SELECT auto_act_enabled FROM rule_groups WHERE id = ? AND enabled = 1`, rid).Scan(&autoAct); err == nil && autoAct != 0 {
			return true
		}
	}
	return false
}

type sqlRows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
}

func (m *Module) loadCollectionByID(ctx context.Context, id string) *storedCollection {
	if id == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadCollectionLocked(ctx, id)
}

func (m *Module) executeAction(ctx context.Context, c storedCandidate) error {
	switch c.ArrAction {
	case ActionDoNothing:
		return nil
	case ActionUnmonitorOnly:
		return m.unmonitorItem(ctx, c, false)
	case ActionUnmonitor:
		return m.unmonitorItem(ctx, c, true)
	case ActionRemoveIfEmpty:
		return m.removeIfEmpty(ctx, c)
	case ActionMove:
		return m.moveItem(ctx, c)
	case ActionChangeQualityProfile:
		return m.changeQualityProfile(ctx, c, c.QualityProfileID)
	default:
		return m.deleteItem(ctx, c)
	}
}

func (m *Module) deleteItem(ctx context.Context, c storedCandidate) error {
	switch c.Scope {
	case ScopeMovieFile:
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.RemoveFile(ctx, &mgmntv1.RemoveFileRequest{FileId: c.ItemID, DeleteFiles: true})
		return err
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.RemoveMovie(ctx, &mgmntv1.RemoveMovieRequest{MovieId: c.ItemID, DeleteFiles: true})
		if err != nil {
			return err
		}
		m.addImportExclusion(ctx, c)
		return nil
	case ScopeSeries:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.RemoveTVShow(ctx, &tvmgmtv1.RemoveTVShowRequest{SeriesId: c.ItemID, DeleteFiles: true})
		if err != nil {
			return err
		}
		m.addImportExclusion(ctx, c)
		return nil
	case ScopeEpisode:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.RemoveEpisodeFile(ctx, &tvmgmtv1.RemoveEpisodeFileRequest{EpisodeId: c.ItemID, DeleteFiles: true})
		return err
	case ScopeSeason:
		return m.deleteSeason(ctx, c)
	default:
		return fmt.Errorf("delete not supported for scope %s", c.Scope)
	}
}

func (m *Module) removeFromLibrary(ctx context.Context, c storedCandidate) error {
	switch c.Scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.RemoveMovie(ctx, &mgmntv1.RemoveMovieRequest{MovieId: c.ItemID, DeleteFiles: false})
		return err
	case ScopeSeries:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.RemoveTVShow(ctx, &tvmgmtv1.RemoveTVShowRequest{SeriesId: c.ItemID, DeleteFiles: false})
		return err
	default:
		return fmt.Errorf("remove not supported for scope %s", c.Scope)
	}
}

func (m *Module) unmonitorItem(ctx context.Context, c storedCandidate, alsoDelete bool) error {
	mon := false
	switch c.Scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.UpdateMovie(ctx, &mgmntv1.UpdateMovieRequest{MovieId: c.ItemID, Monitored: &mon})
		if err != nil {
			return err
		}
		if alsoDelete {
			_, err = mc.RemoveMovie(ctx, &mgmntv1.RemoveMovieRequest{MovieId: c.ItemID, DeleteFiles: true})
		}
		return err
	case ScopeSeries:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.UpdateTVShow(ctx, &tvmgmtv1.UpdateTVShowRequest{SeriesId: c.ItemID, Monitored: &mon})
		if err != nil {
			return err
		}
		if alsoDelete {
			_, err = tc.RemoveTVShow(ctx, &tvmgmtv1.RemoveTVShowRequest{SeriesId: c.ItemID, DeleteFiles: true})
		}
		return err
	case ScopeEpisode:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.UpdateEpisodeMonitored(ctx, &tvmgmtv1.UpdateEpisodeMonitoredRequest{EpisodeId: c.ItemID, Monitored: false})
		if err != nil {
			return err
		}
		if alsoDelete {
			_, err = tc.RemoveEpisodeFile(ctx, &tvmgmtv1.RemoveEpisodeFileRequest{EpisodeId: c.ItemID, DeleteFiles: true})
		}
		return err
	case ScopeSeason:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.UpdateSeasonMonitored(ctx, &tvmgmtv1.UpdateSeasonMonitoredRequest{SeasonId: c.ItemID, Monitored: false})
		if err != nil {
			return err
		}
		if alsoDelete {
			return m.deleteSeason(ctx, c)
		}
		return nil
	default:
		return fmt.Errorf("unmonitor not supported for scope %s", c.Scope)
	}
}

func (m *Module) removeIfEmpty(ctx context.Context, c storedCandidate) error {
	if c.Scope != ScopeSeries {
		return m.deleteItem(ctx, c)
	}
	if err := m.ensureTV(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	tc := m.tvClient
	m.mu.RUnlock()
	resp, err := tc.GetTVShow(ctx, &tvmgmtv1.GetTVShowRequest{SeriesId: c.ItemID})
	if err != nil {
		return err
	}
	hasFiles := false
	for _, s := range resp.GetSeries().GetSeasons() {
		for _, ep := range s.GetEpisodes() {
			if ep.GetHasFile() {
				hasFiles = true
				break
			}
		}
	}
	if hasFiles {
		return m.unmonitorItem(ctx, c, false)
	}
	return m.deleteItem(ctx, c)
}

func (m *Module) removeCandidateFromJellyfin(ctx context.Context, c storedCandidate) {
	ec := parseEvalContext(c.CriteriaJSON)
	m.restoreLeavingSoonOverlay(ctx, ec)
	if c.CollectionID == "" {
		return
	}
	col := m.loadCollectionByID(ctx, c.CollectionID)
	if col == nil {
		return
	}
	m.removeLeavingSoonFromJellyfin(ctx, ec, col.JellyfinCollectionID, col.LeavingSoonLabel)
	m.removeLeavingSoonFromPlex(ctx, ec, col.PlexCollectionKey, col.LeavingSoonLabel)
}

func (m *Module) markCandidateCompleted(ctx context.Context, c storedCandidate) {
	m.removeCandidateFromJellyfin(ctx, c)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx, `UPDATE candidates SET status = ?, completed_at = ?, error = '' WHERE id = ?`, StatusCompleted, nowRFC(), c.ID)
}

func (m *Module) markCandidateFailed(ctx context.Context, id, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx, `UPDATE candidates SET status = ?, error = ?, completed_at = ? WHERE id = ?`, StatusFailed, msg, nowRFC(), id)
}

func (m *Module) startRun(ctx context.Context, kind string, dryRun bool) string {
	id := newID("run")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return id
	}
	_, _ = m.db.ExecContext(ctx, `INSERT INTO run_log (id, kind, status, dry_run, started_at) VALUES (?, ?, 'running', ?, ?)`,
		id, kind, boolToInt(dryRun), nowRFC())
	return id
}

func (m *Module) finishRun(ctx context.Context, id, status string, found, taken, failed int, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx, `UPDATE run_log SET status = ?, candidates_found = ?, actions_taken = ?, actions_failed = ?, error = ?, completed_at = ? WHERE id = ?`,
		status, found, taken, failed, errMsg, nowRFC(), id)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (m *Module) deleteSeason(ctx context.Context, c storedCandidate) error {
	if err := m.ensureTV(ctx); err != nil {
		return err
	}
	ec := parseEvalContext(c.CriteriaJSON)
	if ec.SeriesID == "" {
		return fmt.Errorf("season delete requires series_id in criteria")
	}
	m.mu.RLock()
	tc := m.tvClient
	m.mu.RUnlock()
	resp, err := tc.GetTVShow(ctx, &tvmgmtv1.GetTVShowRequest{SeriesId: ec.SeriesID})
	if err != nil {
		return err
	}
	for _, season := range resp.GetSeries().GetSeasons() {
		if season.GetId() != c.ItemID {
			continue
		}
		for _, ep := range season.GetEpisodes() {
			if !ep.GetHasFile() {
				continue
			}
			if _, err := tc.RemoveEpisodeFile(ctx, &tvmgmtv1.RemoveEpisodeFileRequest{EpisodeId: ep.GetId(), DeleteFiles: true}); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("season %s not found in series %s", c.ItemID, ec.SeriesID)
}

func (m *Module) closeRequestsForItem(ctx context.Context, c storedCandidate) {
	if err := m.ensureRequests(ctx); err != nil {
		return
	}
	m.mu.RLock()
	rc := m.requestClient
	m.mu.RUnlock()
	if rc == nil {
		return
	}
	resp, err := rc.ListRequests(ctx, &requestmedia.ListRequestsRequest{})
	if err != nil {
		return
	}
	for _, r := range resp.GetRequests() {
		if r.GetItemId() != c.ItemID {
			continue
		}
		_, _ = rc.DenyRequest(ctx, &requestmedia.DenyRequestRequest{
			RequestId: r.GetRequestId(),
			Reason:    "denied by " + m.id,
		})
	}
}
