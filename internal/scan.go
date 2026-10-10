package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
)

type storedRule struct {
	Outcome          RuleOutcome
	ArrTag           string
	QualityProfileID string
	Scope            MediaScope
	CollectionID     string
	DefinitionJSON   string
	ArrAction        ArrAction
	ID               string
	Name             string
	MaxActionsPerRun int
	AutoActDelayDays int
	TagEnabled       bool
	AutoActEnabled   bool
	Enabled          bool
}

func (m *Module) loadEnabledRules(ctx context.Context) []storedRule {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, name, enabled, scope, collection_id, definition_json, outcome, arr_action, auto_act_enabled, auto_act_delay_days, tag_enabled, arr_tag, max_actions_per_run, COALESCE(quality_profile_id,'') FROM rule_groups WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var rules []storedRule
	for rows.Next() {
		var r storedRule
		var enabled, autoAct, tagEn int
		if err := rows.Scan(&r.ID, &r.Name, &enabled, &r.Scope, &r.CollectionID, &r.DefinitionJSON, &r.Outcome, &r.ArrAction, &autoAct, &r.AutoActDelayDays, &tagEn, &r.ArrTag, &r.MaxActionsPerRun, &r.QualityProfileID); err != nil {
			continue
		}
		r.Enabled = enabled != 0
		r.AutoActEnabled = autoAct != 0
		r.TagEnabled = tagEn != 0
		rules = append(rules, r)
	}
	return rules
}

func (m *Module) buildEvalContexts(ctx context.Context) ([]EvalContext, error) {
	protected := m.loadProtectionSet(ctx)
	requests := m.loadRequestIndex(ctx)

	var out []EvalContext

	if err := m.ensureMovies(ctx); err == nil {
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		page := int32(1)
		for {
			resp, err := mc.ListMovies(ctx, &mgmntv1.ListMoviesRequest{Page: page, PageSize: 100})
			if err != nil {
				break
			}
			for _, mv := range resp.GetMovies() {
				ws := m.watchStatsForItem(ctx, mv.GetId(), int(mv.GetRuntime()))
				req := requests["movie:"+mv.GetId()]
				if !req.Requested {
					req = requests[fmt.Sprintf("movie:%d", mv.GetTmdbId())]
				}
				added := parseTimeRFC(mv.GetCreatedAt())
				files := m.listMovieFiles(ctx, mv.GetId())
				ctxItem := EvalContext{
					Scope:             ScopeMovie,
					ItemID:            mv.GetId(),
					Title:             mv.GetTitle(),
					Year:              int(mv.GetYear()),
					TmdbID:            int(mv.GetTmdbId()),
					ImdbID:            mv.GetImdbId(),
					Genres:            mv.GetGenres(),
					Monitored:         mv.GetMonitored(),
					HasFile:           mv.GetHasFile(),
					AddedAt:           added,
					VoteAverage:       mv.GetVoteAverage(),
					QualityProfile:    mv.GetQualityProfileId(),
					RootFolderPath:    mv.GetRootFolderPath(),
					Requested:         req.Requested,
					RequestedBy:       req.RequestedBy,
					DaysSinceRequest:  daysSince(req.CreatedAt),
					DiskFreePercent:   diskFreePercent(mv.GetRootFolderPath()),
					Protected:         protected[string(ScopeMovie)+":"+mv.GetId()],
					RuntimeMinutes:    int(mv.GetRuntime()),
					MovieVersionCount: len(files),
				}
				applyWatchStats(&ctxItem, ws)
				if req.Requested && req.RequestedBy != "" {
					ctxItem.RequesterWatched = m.requesterHasWatched(mv.GetId(), req.RequestedBy)
				}
				if ctxItem.HasFile {
					ctxItem.FileSizeBytes = m.movieFileSize(ctx, mv.GetId())
				}
				m.enrichRatings(ctx, &ctxItem)
				out = append(out, ctxItem)
				for _, f := range files {
					fileCtx := ctxItem
					fileCtx.Scope = ScopeMovieFile
					fileCtx.ItemID = f.ID
					fileCtx.MovieID = mv.GetId()
					fileCtx.Title = fmt.Sprintf("%s (%s)", mv.GetTitle(), f.Quality)
					fileCtx.FilePath = f.Path
					fileCtx.FileQuality = f.Quality
					fileCtx.FileSizeBytes = f.SizeBytes
					fileCtx.HasFile = true
					fileCtx.Protected = protected[string(ScopeMovieFile)+":"+f.ID]
					applyMediaProbe(&fileCtx, f.Path, f.Quality, f.Container)
					m.enrichFileProbe(ctx, &fileCtx)
					out = append(out, fileCtx)
				}
			}
			if int32(len(resp.GetMovies())) < 100 || page*100 >= resp.GetTotal() { //nolint:gosec // page sizes are bounded list requests
				break
			}
			page++
		}
	}

	if err := m.ensureTV(ctx); err == nil {
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		page := int32(1)
		for {
			resp, listErr := tc.ListTVShows(ctx, &tvmgmtv1.ListTVShowsRequest{Page: page, PageSize: 50})
			if listErr != nil {
				break
			}
			for _, sr := range resp.GetSeries() {
				seriesCtx := EvalContext{
					Scope:          ScopeSeries,
					ItemID:         sr.GetId(),
					Title:          sr.GetName(),
					Year:           int(sr.GetYear()),
					TmdbID:         int(sr.GetTmdbId()),
					Genres:         sr.GetGenres(),
					Monitored:      sr.GetMonitored(),
					HasFile:        sr.GetTotalEpisodes() > 0,
					AddedAt:        parseTimeRFC(sr.GetCreatedAt()),
					VoteAverage:    sr.GetVoteAverage(),
					QualityProfile: sr.GetQualityProfileId(),
					RootFolderPath: sr.GetRootFolderPath(),
					SeriesType:     sr.GetSeriesType(),
					SeriesStatus:   sr.GetStatus(),
					FirstAirDate:   parseDateYMD(sr.GetFirstAirDate()),
					LastAirDate:    parseDateYMD(sr.GetLastAirDate()),
					SeasonCount:    len(sr.GetSeasons()),
					Protected:      protected[string(ScopeSeries)+":"+sr.GetId()],
				}
				m.enrichRatings(ctx, &seriesCtx)
				applyWatchStats(&seriesCtx, m.watchStatsForItem(ctx, sr.GetId(), 0))
				req := requests["tv:"+sr.GetId()]
				seriesCtx.Requested = req.Requested
				seriesCtx.RequestedBy = req.RequestedBy
				seriesCtx.DaysSinceRequest = daysSince(req.CreatedAt)
				if req.Requested && req.RequestedBy != "" {
					seriesCtx.RequesterWatched = m.requesterHasWatched(sr.GetId(), req.RequestedBy)
				}
				seriesCtx.DiskFreePercent = diskFreePercent(sr.GetRootFolderPath())
				out = append(out, seriesCtx)

				for _, season := range sr.GetSeasons() {
					seasonCtx := seriesCtx
					seasonCtx.Scope = ScopeSeason
					seasonCtx.ItemID = season.GetId()
					seasonCtx.Title = fmt.Sprintf("%s S%02d", sr.GetName(), season.GetSeasonNumber())
					seasonCtx.SeasonNumber = int(season.GetSeasonNumber())
					seasonCtx.Monitored = season.GetMonitored()
					seasonCtx.FirstAirDate = parseDateYMD(season.GetAirDate())
					seasonCtx.Protected = protected[string(ScopeSeason)+":"+season.GetId()]
					var seasonSize int64

					for _, ep := range season.GetEpisodes() {
						if !ep.GetHasFile() {
							continue
						}
						epCtx := seasonCtx
						epCtx.Scope = ScopeEpisode
						epCtx.ItemID = ep.GetId()
						epCtx.Title = fmt.Sprintf("%s S%02dE%02d", sr.GetName(), ep.GetSeasonNumber(), ep.GetEpisodeNumber())
						epCtx.EpisodeNumber = int(ep.GetEpisodeNumber())
						epCtx.SeriesID = sr.GetId()
						epCtx.Monitored = ep.GetMonitored()
						epCtx.HasFile = ep.GetHasFile()
						epCtx.EpisodeAirDate = parseDateYMD(ep.GetAirDate())
						epCtx.Protected = protected[string(ScopeEpisode)+":"+ep.GetId()]
						applyWatchStats(&epCtx, m.watchStatsForItem(ctx, ep.GetId(), 0))
						if f := m.episodeFileDetails(ctx, epCtx.TmdbID, epCtx.SeasonNumber, epCtx.EpisodeNumber, ep.GetId()); f.Path != "" || f.SizeBytes > 0 {
							epCtx.FilePath = f.Path
							epCtx.FileSizeBytes = f.SizeBytes
							epCtx.FileQuality = f.Quality
							applyMediaProbe(&epCtx, f.Path, f.Quality, f.Container)
							m.enrichFileProbe(ctx, &epCtx)
							seasonSize += f.SizeBytes
						}
						out = append(out, epCtx)
					}
					seasonCtx.FileSizeBytes = seasonSize
					out = append(out, seasonCtx)
				}
			}
			if int32(len(resp.GetSeries())) < 50 || page*50 >= resp.GetTotal() { //nolint:gosec // page sizes are bounded list requests
				break
			}
			page++
		}
	}
	return out, nil //nolint:nilerr // partial inventory is returned when one arr client fails
}

func parseDateYMD(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	return parseTimeRFC(s)
}

func (m *Module) movieFileSize(ctx context.Context, movieID string) int64 {
	m.mu.RLock()
	mc := m.moviesClient
	m.mu.RUnlock()
	if mc == nil {
		return 0
	}
	resp, err := mc.ListFiles(ctx, &mgmntv1.ListFilesRequest{MovieId: movieID})
	if err != nil {
		return 0
	}
	var total int64
	for _, f := range resp.GetFiles() {
		total += f.GetSizeBytes()
	}
	return total
}

type movieFileInfo struct {
	ID        string
	Path      string
	Quality   string
	Container string
	SizeBytes int64
}

func (m *Module) listMovieFiles(ctx context.Context, movieID string) []movieFileInfo {
	m.mu.RLock()
	mc := m.moviesClient
	m.mu.RUnlock()
	if mc == nil {
		return nil
	}
	resp, err := mc.ListFiles(ctx, &mgmntv1.ListFilesRequest{MovieId: movieID})
	if err != nil {
		return nil
	}
	var out []movieFileInfo
	for _, f := range resp.GetFiles() {
		out = append(out, movieFileInfo{
			ID:        f.GetId(),
			Path:      f.GetFilePath(),
			Quality:   f.GetQuality(),
			Container: f.GetContainer(),
			SizeBytes: f.GetSizeBytes(),
		})
	}
	return out
}

func (m *Module) loadProtectionSet(ctx context.Context) map[string]bool {
	out := make(map[string]bool)
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return out
	}
	now := time.Now().UTC()
	rows, err := db.QueryContext(ctx, `SELECT scope, item_id, expires_at FROM protections`)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scope, itemID, expires string
		if err := rows.Scan(&scope, &itemID, &expires); err != nil {
			continue
		}
		if expires != "" {
			if t, err := time.Parse(time.RFC3339, expires); err == nil && t.Before(now) {
				continue
			}
		}
		out[scope+":"+itemID] = true
	}
	return out
}

func (m *Module) evaluateRules(ctx context.Context, rules []storedRule, contexts []EvalContext, excluded map[int]struct{}, importExcluded map[string]struct{}, jwPolicies []justWatchPolicy) map[string]candidateMatch {
	matches := make(map[string]candidateMatch)
	for _, rule := range rules {
		def, err := parseRuleDefinition(rule.DefinitionJSON)
		if err != nil {
			slog.Debug("skip rule with bad definition", "rule", rule.ID, "error", err)
			continue
		}
		for _, ec := range contexts {
			if rule.Scope != "" && ec.Scope != rule.Scope {
				continue
			}
			if m.isExcludedByList(ec.TmdbID, excluded) {
				continue
			}
			if m.isImportExcluded(ec.Scope, ec.TmdbID, importExcluded) {
				continue
			}
			if m.isExcludedByJustWatch(ctx, ec, jwPolicies) {
				continue
			}
			if m.shouldProtectUnwatchedRequester(ec) {
				m.upsertProtection(ctx, ec, "requester_unwatched", "")
				continue
			}
			ok, err := evaluateRule(def, ec)
			if err != nil || !ok {
				continue
			}
			key := string(ec.Scope) + ":" + ec.ItemID
			if rule.Outcome == OutcomeProtect {
				m.upsertProtection(ctx, ec, "rule:"+rule.ID, "")
				continue
			}
			cur, exists := matches[key]
			if !exists {
				cur = candidateMatch{Ctx: ec, RuleIDs: []string{rule.ID}, Action: rule.ArrAction}
			} else {
				cur.RuleIDs = append(cur.RuleIDs, rule.ID)
				cur.Action = mergeArrAction(cur.Action, rule.ArrAction)
			}
			if rule.CollectionID != "" {
				cur.CollectionID = rule.CollectionID
			}
			if rule.AutoActEnabled {
				cur.AutoActDelayDays = rule.AutoActDelayDays
			}
			if rule.TagEnabled && rule.ArrTag != "" {
				cur.TagEnabled = true
				cur.ArrTag = rule.ArrTag
			}
			if rule.QualityProfileID != "" {
				cur.QualityProfileID = rule.QualityProfileID
			}
			matches[key] = cur
		}
	}
	return matches
}

type candidateMatch struct {
	Action           ArrAction
	CollectionID     string
	ArrTag           string
	QualityProfileID string
	RuleIDs          []string
	Ctx              EvalContext
	AutoActDelayDays int
	TagEnabled       bool
}

func (m *Module) upsertProtection(ctx context.Context, ec EvalContext, reason, expiresAt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	id := fmt.Sprintf("prot_%s_%s", ec.Scope, ec.ItemID)
	now := nowRFC()
	_, _ = m.db.ExecContext(ctx, `INSERT INTO protections (id, scope, item_id, title, reason, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, item_id) DO UPDATE SET title=excluded.title, reason=excluded.reason, expires_at=excluded.expires_at`,
		id, ec.Scope, ec.ItemID, ec.Title, reason, expiresAt, now)
}

func (m *Module) persistCandidates(ctx context.Context, matches map[string]candidateMatch, dryRun bool) int {
	if dryRun {
		return len(matches)
	}
	count := 0
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return 0
	}
	// ADR-0035: never re-persist a user id the identity ledger has erased.
	// Upstream owners may still carry it until they apply the same tombstone;
	// the candidate is regenerated once they have. Read under m.mu, which
	// eraseUser also holds, so an erasure cannot land between this read and
	// the inserts below.
	erased, erasedErr := erasedUserIDs(ctx, m.db)
	if erasedErr != nil {
		slog.Warn("persist candidates skipped: cannot read erasure records", "error", erasedErr)
		return 0
	}
	for _, match := range matches {
		if match.Ctx.Protected {
			continue
		}
		ruleIDs, _ := json.Marshal(match.RuleIDs)
		criteria, _ := json.Marshal(match.Ctx)
		if criteriaNameErasedUser(string(criteria), erased) {
			slog.Debug("candidate not persisted: names an erased user", "item", match.Ctx.ItemID)
			continue
		}
		now := nowRFC()
		actAfter := now
		status := StatusPending
		if match.CollectionID != "" {
			if col := m.loadCollectionLocked(ctx, match.CollectionID); col != nil {
				if col.GraceDays > 0 {
					actAfter = time.Now().UTC().Add(time.Duration(col.GraceDays) * 24 * time.Hour).Format(time.RFC3339)
				}
				if col.LeavingSoonEnabled {
					status = StatusLeavingSoon
				}
			}
		} else if match.AutoActDelayDays > 0 {
			actAfter = time.Now().UTC().Add(time.Duration(match.AutoActDelayDays) * 24 * time.Hour).Format(time.RFC3339)
		}
		id := newID("cand")
		res, err := m.db.ExecContext(ctx, `INSERT INTO candidates (id, scope, item_id, title, year, tmdb_id, imdb_id, matched_rule_ids, arr_action, status, collection_id, added_at, act_after, criteria_json, size_bytes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(scope, item_id) DO UPDATE SET
				matched_rule_ids=excluded.matched_rule_ids,
				arr_action=excluded.arr_action,
				status=CASE WHEN candidates.status IN ('completed','cancelled') THEN excluded.status ELSE candidates.status END,
				collection_id=excluded.collection_id,
				act_after=CASE
					WHEN candidates.status IN ('completed','cancelled') THEN excluded.act_after
					WHEN candidates.act_after != '' THEN candidates.act_after
					ELSE excluded.act_after
				END,
				criteria_json=excluded.criteria_json,
				size_bytes=excluded.size_bytes`,
			id, match.Ctx.Scope, match.Ctx.ItemID, match.Ctx.Title, match.Ctx.Year, match.Ctx.TmdbID, match.Ctx.ImdbID,
			string(ruleIDs), match.Action, status, match.CollectionID, now, actAfter, string(criteria), match.Ctx.FileSizeBytes)
		if err != nil {
			slog.Debug("persist candidate failed", "item", match.Ctx.ItemID, "error", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			count++
			if match.TagEnabled {
				m.applyCandidateTag(ctx, match)
			}
			if status == StatusLeavingSoon && match.CollectionID != "" {
				if col := m.loadCollectionLocked(ctx, match.CollectionID); col != nil {
					label := col.LeavingSoonLabel
					if label == "" {
						label = "Leaving Soon"
					}
					m.syncLeavingSoonToJellyfin(ctx, match.Ctx, col)
					m.syncLeavingSoonToPlex(ctx, match.Ctx, col)
					m.applyLeavingSoonOverlay(ctx, match.Ctx, label, actAfter)
					m.notifyLeavingSoon(ctx, match.Ctx, actAfter, label)
				}
			}
		}
	}
	return count
}

type storedCollection struct {
	ID                   string
	Name                 string
	ArrAction            ArrAction
	LeavingSoonLabel     string
	JellyfinCollectionID string
	PlexCollectionKey    string
	GraceDays            int
	Enabled              bool
	LeavingSoonEnabled   bool
}

func (m *Module) loadCollectionLocked(ctx context.Context, id string) *storedCollection {
	row := m.db.QueryRowContext(ctx, `SELECT id, name, enabled, grace_days, arr_action, leaving_soon_enabled, leaving_soon_label, COALESCE(jellyfin_collection_id,''), COALESCE(plex_collection_key,'') FROM collections WHERE id = ?`, id)
	var c storedCollection
	var enabled, leaving int
	if err := row.Scan(&c.ID, &c.Name, &enabled, &c.GraceDays, &c.ArrAction, &leaving, &c.LeavingSoonLabel, &c.JellyfinCollectionID, &c.PlexCollectionKey); err != nil {
		return nil
	}
	c.Enabled = enabled != 0
	c.LeavingSoonEnabled = leaving != 0
	return &c
}

func (m *Module) runScan(ctx context.Context, dryRun bool) (int, error) {
	_, _, _ = m.syncExclusionLists(ctx)
	rules := m.loadEnabledRules(ctx)
	contexts, err := m.buildEvalContexts(ctx)
	if err != nil {
		return 0, err
	}
	excluded := m.loadExclusionTMDBSet(ctx)
	importExcluded := m.loadImportExclusionSet(ctx)
	jwPolicies := m.loadJustWatchPolicies(ctx)
	matches := m.evaluateRules(ctx, rules, contexts, excluded, importExcluded, jwPolicies)
	return m.persistCandidates(ctx, matches, dryRun), nil
}
