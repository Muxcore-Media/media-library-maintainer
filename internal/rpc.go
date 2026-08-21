package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

func (m *Module) ListRules(ctx context.Context, req *maintainv1.ListRulesRequest) (*maintainv1.ListRulesResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return &maintainv1.ListRulesResponse{}, nil
	}
	rows, err := m.db.Query(`SELECT id, name, enabled, scope, collection_id, definition_json, outcome, arr_action, auto_act_enabled, auto_act_delay_days, tag_enabled, arr_tag, max_actions_per_run, COALESCE(quality_profile_id,''), created_at, updated_at FROM rule_groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []*maintainv1.RuleGroup
	for rows.Next() {
		r, err := scanRuleRow(rows)
		if err != nil {
			continue
		}
		rules = append(rules, r)
	}
	return &maintainv1.ListRulesResponse{Rules: rules}, nil
}

func (m *Module) GetRule(ctx context.Context, req *maintainv1.GetRuleRequest) (*maintainv1.GetRuleResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row := m.db.QueryRow(`SELECT id, name, enabled, scope, collection_id, definition_json, outcome, arr_action, auto_act_enabled, auto_act_delay_days, tag_enabled, arr_tag, max_actions_per_run, COALESCE(quality_profile_id,''), created_at, updated_at FROM rule_groups WHERE id = ?`, req.GetId())
	r, err := scanRuleRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.GetRuleResponse{Rule: r}, nil
}

func (m *Module) UpsertRule(ctx context.Context, req *maintainv1.UpsertRuleRequest) (*maintainv1.UpsertRuleResponse, error) {
	in := req.GetRule()
	if in == nil {
		return nil, fmt.Errorf("rule required")
	}
	now := nowRFC()
	id := in.GetId()
	if id == "" {
		id = newID("rule")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`INSERT INTO rule_groups (id, name, enabled, scope, collection_id, definition_json, outcome, arr_action, auto_act_enabled, auto_act_delay_days, tag_enabled, arr_tag, max_actions_per_run, quality_profile_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, enabled=excluded.enabled, scope=excluded.scope, collection_id=excluded.collection_id,
			definition_json=excluded.definition_json, outcome=excluded.outcome, arr_action=excluded.arr_action,
			auto_act_enabled=excluded.auto_act_enabled, auto_act_delay_days=excluded.auto_act_delay_days,
			tag_enabled=excluded.tag_enabled, arr_tag=excluded.arr_tag, max_actions_per_run=excluded.max_actions_per_run,
			quality_profile_id=excluded.quality_profile_id, updated_at=excluded.updated_at`,
		id, in.GetName(), boolToInt(in.GetEnabled()), protoScope(in.GetScope()), in.GetCollectionId(), in.GetDefinitionJson(),
		protoOutcome(in.GetOutcome()), protoAction(in.GetArrAction()), boolToInt(in.GetAutoActEnabled()), in.GetAutoActDelayDays(),
		boolToInt(in.GetTagEnabled()), in.GetArrTag(), in.GetMaxActionsPerRun(), in.GetQualityProfileId(), now, now)
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`SELECT id, name, enabled, scope, collection_id, definition_json, outcome, arr_action, auto_act_enabled, auto_act_delay_days, tag_enabled, arr_tag, max_actions_per_run, COALESCE(quality_profile_id,''), created_at, updated_at FROM rule_groups WHERE id = ?`, id)
	r, err := scanRuleRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.UpsertRuleResponse{Rule: r}, nil
}

func (m *Module) DeleteRule(ctx context.Context, req *maintainv1.DeleteRuleRequest) (*maintainv1.DeleteRuleResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`DELETE FROM rule_groups WHERE id = ?`, req.GetId())
	return &maintainv1.DeleteRuleResponse{}, err
}

func (m *Module) PreviewRule(ctx context.Context, req *maintainv1.PreviewRuleRequest) (*maintainv1.PreviewRuleResponse, error) {
	in := req.GetRule()
	if in == nil {
		return nil, fmt.Errorf("rule required")
	}
	rule := storedRule{
		ID:             in.GetId(),
		Scope:          protoScope(in.GetScope()),
		DefinitionJSON: in.GetDefinitionJson(),
		Outcome:        protoOutcome(in.GetOutcome()),
		ArrAction:      protoAction(in.GetArrAction()),
		Enabled:        true,
	}
	contexts, err := m.buildEvalContexts(ctx)
	if err != nil {
		return nil, err
	}
	def, err := parseRuleDefinition(rule.DefinitionJSON)
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	excluded := m.loadExclusionTMDBSet()
	importExcluded := m.loadImportExclusionSet()
	jwPolicies := m.loadJustWatchPolicies()
	var matches []*maintainv1.Candidate
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
		ok, err := evaluateRule(def, ec)
		if err != nil || !ok {
			continue
		}
		matches = append(matches, evalToCandidate(ec, rule.ArrAction, nil))
		if len(matches) >= limit {
			break
		}
	}
	return &maintainv1.PreviewRuleResponse{Matches: matches, Total: int32(len(matches))}, nil
}

func (m *Module) ListCollections(ctx context.Context, req *maintainv1.ListCollectionsRequest) (*maintainv1.ListCollectionsResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows, err := m.db.Query(`SELECT id, name, enabled, grace_days, arr_action, leaving_soon_enabled, leaving_soon_label, created_at, updated_at FROM collections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []*maintainv1.Collection
	for rows.Next() {
		c, err := scanCollectionRow(rows)
		if err != nil {
			continue
		}
		cols = append(cols, c)
	}
	return &maintainv1.ListCollectionsResponse{Collections: cols}, nil
}

func (m *Module) UpsertCollection(ctx context.Context, req *maintainv1.UpsertCollectionRequest) (*maintainv1.UpsertCollectionResponse, error) {
	in := req.GetCollection()
	if in == nil {
		return nil, fmt.Errorf("collection required")
	}
	now := nowRFC()
	id := in.GetId()
	if id == "" {
		id = newID("col")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`INSERT INTO collections (id, name, enabled, grace_days, arr_action, leaving_soon_enabled, leaving_soon_label, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, enabled=excluded.enabled, grace_days=excluded.grace_days,
			arr_action=excluded.arr_action, leaving_soon_enabled=excluded.leaving_soon_enabled, leaving_soon_label=excluded.leaving_soon_label, updated_at=excluded.updated_at`,
		id, in.GetName(), boolToInt(in.GetEnabled()), in.GetGraceDays(), protoAction(in.GetArrAction()),
		boolToInt(in.GetLeavingSoonEnabled()), in.GetLeavingSoonLabel(), now, now)
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`SELECT id, name, enabled, grace_days, arr_action, leaving_soon_enabled, leaving_soon_label, created_at, updated_at FROM collections WHERE id = ?`, id)
	c, err := scanCollectionRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.UpsertCollectionResponse{Collection: c}, nil
}

func (m *Module) DeleteCollection(ctx context.Context, req *maintainv1.DeleteCollectionRequest) (*maintainv1.DeleteCollectionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`DELETE FROM collections WHERE id = ?`, req.GetId())
	return &maintainv1.DeleteCollectionResponse{}, err
}

func (m *Module) ListCandidates(ctx context.Context, req *maintainv1.ListCandidatesRequest) (*maintainv1.ListCandidatesResponse, error) {
	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	m.mu.RLock()
	defer m.mu.RUnlock()
	query := `SELECT id, scope, item_id, title, year, tmdb_id, imdb_id, matched_rule_ids, arr_action, status, collection_id, added_at, act_after, postponed_until, completed_at, error, criteria_json, size_bytes FROM candidates`
	countQ := `SELECT COUNT(*) FROM candidates`
	var args []any
	var where []string
	if req.GetStatus() != maintainv1.CandidateStatus_CANDIDATE_STATUS_UNSPECIFIED {
		where = append(where, `status = ?`)
		args = append(args, protoCandidateStatus(req.GetStatus()))
	}
	if req.GetScope() != maintainv1.MediaScope_MEDIA_SCOPE_UNSPECIFIED {
		where = append(where, `scope = ?`)
		args = append(args, protoScope(req.GetScope()))
	}
	if len(where) > 0 {
		clause := ` WHERE ` + strings.Join(where, ` AND `)
		query += clause
		countQ += clause
	}
	var total int
	_ = m.db.QueryRow(countQ, args...).Scan(&total)
	query += ` ORDER BY added_at DESC LIMIT ? OFFSET ?`
	qargs := append(args, pageSize, offset)
	rows, err := m.db.Query(query, qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cands []*maintainv1.Candidate
	for rows.Next() {
		c, err := scanCandidateRow(rows)
		if err != nil {
			continue
		}
		cands = append(cands, c)
	}
	return &maintainv1.ListCandidatesResponse{Candidates: cands, Total: int32(total), Page: int32(page), PageSize: int32(pageSize)}, nil
}

func (m *Module) GetCandidate(ctx context.Context, req *maintainv1.GetCandidateRequest) (*maintainv1.GetCandidateResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row := m.db.QueryRow(`SELECT id, scope, item_id, title, year, tmdb_id, imdb_id, matched_rule_ids, arr_action, status, collection_id, added_at, act_after, postponed_until, completed_at, error, criteria_json, size_bytes FROM candidates WHERE id = ?`, req.GetId())
	c, err := scanCandidateRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.GetCandidateResponse{Candidate: c}, nil
}

func (m *Module) ApproveCandidate(ctx context.Context, req *maintainv1.ApproveCandidateRequest) (*maintainv1.ApproveCandidateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`UPDATE candidates SET status = ?, act_after = ? WHERE id = ?`, StatusApproved, nowRFC(), req.GetId())
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`SELECT id, scope, item_id, title, year, tmdb_id, imdb_id, matched_rule_ids, arr_action, status, collection_id, added_at, act_after, postponed_until, completed_at, error, criteria_json, size_bytes FROM candidates WHERE id = ?`, req.GetId())
	c, err := scanCandidateRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.ApproveCandidateResponse{Candidate: c}, nil
}

func (m *Module) PostponeCandidate(ctx context.Context, req *maintainv1.PostponeCandidateRequest) (*maintainv1.PostponeCandidateResponse, error) {
	days := int(req.GetDays())
	if days <= 0 {
		days = 7
	}
	until := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`UPDATE candidates SET status = ?, postponed_until = ? WHERE id = ?`, StatusPostponed, until, req.GetId())
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`SELECT id, scope, item_id, title, year, tmdb_id, imdb_id, matched_rule_ids, arr_action, status, collection_id, added_at, act_after, postponed_until, completed_at, error, criteria_json, size_bytes FROM candidates WHERE id = ?`, req.GetId())
	c, err := scanCandidateRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.PostponeCandidateResponse{Candidate: c}, nil
}

func (m *Module) CancelCandidate(ctx context.Context, req *maintainv1.CancelCandidateRequest) (*maintainv1.CancelCandidateResponse, error) {
	m.mu.RLock()
	row := m.db.QueryRow(`SELECT id, scope, item_id, title, arr_action, status, collection_id, act_after, postponed_until, size_bytes, criteria_json FROM candidates WHERE id = ?`, req.GetId())
	var c storedCandidate
	scanErr := row.Scan(&c.ID, &c.Scope, &c.ItemID, &c.Title, &c.ArrAction, &c.Status, &c.CollectionID, &c.ActAfter, &c.PostponedUntil, &c.SizeBytes, &c.CriteriaJSON)
	m.mu.RUnlock()
	if scanErr == nil {
		m.removeCandidateFromJellyfin(ctx, c)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`UPDATE candidates SET status = ? WHERE id = ?`, StatusCancelled, req.GetId())
	if err != nil {
		return nil, err
	}
	outRow := m.db.QueryRow(`SELECT id, scope, item_id, title, year, tmdb_id, imdb_id, matched_rule_ids, arr_action, status, collection_id, added_at, act_after, postponed_until, completed_at, error, criteria_json, size_bytes FROM candidates WHERE id = ?`, req.GetId())
	cand, err := scanCandidateRow(outRow)
	if err != nil {
		return nil, err
	}
	return &maintainv1.CancelCandidateResponse{Candidate: cand}, nil
}

func (m *Module) ListProtections(ctx context.Context, req *maintainv1.ListProtectionsRequest) (*maintainv1.ListProtectionsResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows, err := m.db.Query(`SELECT id, scope, item_id, title, reason, expires_at, created_at FROM protections ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*maintainv1.Protection
	for rows.Next() {
		p, err := scanProtectionRow(rows)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return &maintainv1.ListProtectionsResponse{Protections: out}, nil
}

func (m *Module) UpsertProtection(ctx context.Context, req *maintainv1.UpsertProtectionRequest) (*maintainv1.UpsertProtectionResponse, error) {
	in := req.GetProtection()
	if in == nil {
		return nil, fmt.Errorf("protection required")
	}
	id := in.GetId()
	if id == "" {
		id = newID("prot")
	}
	now := nowRFC()
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`INSERT INTO protections (id, scope, item_id, title, reason, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, item_id) DO UPDATE SET title=excluded.title, reason=excluded.reason, expires_at=excluded.expires_at`,
		id, protoScope(in.GetScope()), in.GetItemId(), in.GetTitle(), in.GetReason(), in.GetExpiresAt(), now)
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`SELECT id, scope, item_id, title, reason, expires_at, created_at FROM protections WHERE scope = ? AND item_id = ?`, protoScope(in.GetScope()), in.GetItemId())
	p, err := scanProtectionRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.UpsertProtectionResponse{Protection: p}, nil
}

func (m *Module) DeleteProtection(ctx context.Context, req *maintainv1.DeleteProtectionRequest) (*maintainv1.DeleteProtectionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`DELETE FROM protections WHERE id = ?`, req.GetId())
	return &maintainv1.DeleteProtectionResponse{}, err
}

func (m *Module) ScanNow(ctx context.Context, req *maintainv1.ScanNowRequest) (*maintainv1.ScanNowResponse, error) {
	runID := m.startRun("scan", req.GetDryRun())
	found, err := m.runScan(ctx, req.GetDryRun())
	status := "completed"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	m.finishRun(runID, status, found, 0, 0, errMsg)
	m.notifyRun("scan", found, 0, 0, req.GetDryRun(), errMsg)
	return &maintainv1.ScanNowResponse{
		Run:              &maintainv1.RunLog{Id: runID, Kind: "scan", Status: status, CandidatesFound: int32(found), DryRun: req.GetDryRun(), Error: errMsg},
		CandidatesFound:  int32(found),
	}, err
}

func (m *Module) ActNow(ctx context.Context, req *maintainv1.ActNowRequest) (*maintainv1.ActNowResponse, error) {
	runID := m.startRun("act", req.GetDryRun())
	taken, failed, err := m.runAct(ctx, actOptions{
		dryRun:            req.GetDryRun(),
		maxActions:        int(req.GetMaxActions()),
		freeUp:            req.GetFreeUp(),
		targetFreePercent: req.GetTargetFreePercent(),
	})
	status := "completed"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	m.finishRun(runID, status, 0, taken, failed, errMsg)
	m.notifyRun("act", 0, taken, failed, req.GetDryRun(), errMsg)
	return &maintainv1.ActNowResponse{
		Run:           &maintainv1.RunLog{Id: runID, Kind: "act", Status: status, ActionsTaken: int32(taken), ActionsFailed: int32(failed), DryRun: req.GetDryRun(), Error: errMsg},
		ActionsTaken: int32(taken),
	}, err
}

func (m *Module) ListRuns(ctx context.Context, req *maintainv1.ListRunsRequest) (*maintainv1.ListRunsResponse, error) {
	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM run_log`).Scan(&total)
	rows, err := m.db.Query(`SELECT id, kind, status, candidates_found, actions_taken, actions_failed, dry_run, error, started_at, completed_at FROM run_log ORDER BY started_at DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*maintainv1.RunLog
	for rows.Next() {
		r, err := scanRunRow(rows)
		if err != nil {
			continue
		}
		runs = append(runs, r)
	}
	return &maintainv1.ListRunsResponse{Runs: runs, Total: int32(total)}, nil
}

func scanRuleRow(s scanner) (*maintainv1.RuleGroup, error) {
	var id, name, scope, colID, def, outcome, action, tag, profileID, created, updated string
	var enabled, autoAct, tagEn, maxAct int
	var delay int
	if err := s.Scan(&id, &name, &enabled, &scope, &colID, &def, &outcome, &action, &autoAct, &delay, &tagEn, &tag, &maxAct, &profileID, &created, &updated); err != nil {
		return nil, err
	}
	return &maintainv1.RuleGroup{
		Id: id, Name: name, Enabled: enabled != 0, Scope: toProtoScope(MediaScope(scope)), CollectionId: colID,
		DefinitionJson: def, Outcome: toProtoOutcome(RuleOutcome(outcome)), ArrAction: toProtoAction(ArrAction(action)),
		AutoActEnabled: autoAct != 0, AutoActDelayDays: int32(delay), TagEnabled: tagEn != 0, ArrTag: tag,
		MaxActionsPerRun: int32(maxAct), QualityProfileId: profileID, CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func scanCollectionRow(s scanner) (*maintainv1.Collection, error) {
	var id, name, action, label, created, updated string
	var enabled, grace, leaving int
	if err := s.Scan(&id, &name, &enabled, &grace, &action, &leaving, &label, &created, &updated); err != nil {
		return nil, err
	}
	return &maintainv1.Collection{
		Id: id, Name: name, Enabled: enabled != 0, GraceDays: int32(grace), ArrAction: toProtoAction(ArrAction(action)),
		LeavingSoonEnabled: leaving != 0, LeavingSoonLabel: label, CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func scanCandidateRow(s scanner) (*maintainv1.Candidate, error) {
	var id, scope, itemID, title, imdb, rules, action, status, colID, added, actAfter, postponed, completed, errStr, criteria string
	var year, tmdb int
	var size int64
	if err := s.Scan(&id, &scope, &itemID, &title, &year, &tmdb, &imdb, &rules, &action, &status, &colID, &added, &actAfter, &postponed, &completed, &errStr, &criteria, &size); err != nil {
		return nil, err
	}
	var ruleIDs []string
	_ = json.Unmarshal([]byte(rules), &ruleIDs)
	return &maintainv1.Candidate{
		Id: id, Scope: toProtoScope(MediaScope(scope)), ItemId: itemID, Title: title, Year: int32(year),
		TmdbId: int32(tmdb), ImdbId: imdb, MatchedRuleIds: ruleIDs, ArrAction: toProtoAction(ArrAction(action)),
		Status: toProtoCandidateStatus(CandidateStatus(status)), CollectionId: colID, AddedAt: added,
		ActAfter: actAfter, PostponedUntil: postponed, CompletedAt: completed, Error: errStr,
		CriteriaJson: criteria, SizeBytes: size,
	}, nil
}

func scanProtectionRow(s scanner) (*maintainv1.Protection, error) {
	var id, scope, itemID, title, reason, expires, created string
	if err := s.Scan(&id, &scope, &itemID, &title, &reason, &expires, &created); err != nil {
		return nil, err
	}
	return &maintainv1.Protection{
		Id: id, Scope: toProtoScope(MediaScope(scope)), ItemId: itemID, Title: title,
		Reason: reason, ExpiresAt: expires, CreatedAt: created,
	}, nil
}

func scanRunRow(s scanner) (*maintainv1.RunLog, error) {
	var id, kind, status, errStr, started, completed string
	var found, taken, failed, dry int
	if err := s.Scan(&id, &kind, &status, &found, &taken, &failed, &dry, &errStr, &started, &completed); err != nil {
		return nil, err
	}
	return &maintainv1.RunLog{
		Id: id, Kind: kind, Status: status, CandidatesFound: int32(found), ActionsTaken: int32(taken),
		ActionsFailed: int32(failed), DryRun: dry != 0, Error: errStr, StartedAt: started, CompletedAt: completed,
	}, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func evalToCandidate(ec EvalContext, action ArrAction, ruleIDs []string) *maintainv1.Candidate {
	criteria, _ := json.Marshal(ec)
	return &maintainv1.Candidate{
		Scope: toProtoScope(ec.Scope), ItemId: ec.ItemID, Title: ec.Title, Year: int32(ec.Year),
		TmdbId: int32(ec.TmdbID), ImdbId: ec.ImdbID, MatchedRuleIds: ruleIDs, ArrAction: toProtoAction(action),
		CriteriaJson: string(criteria), SizeBytes: ec.FileSizeBytes,
	}
}

func protoScope(s maintainv1.MediaScope) MediaScope {
	switch s {
	case maintainv1.MediaScope_MEDIA_SCOPE_MOVIE:
		return ScopeMovie
	case maintainv1.MediaScope_MEDIA_SCOPE_SERIES:
		return ScopeSeries
	case maintainv1.MediaScope_MEDIA_SCOPE_SEASON:
		return ScopeSeason
	case maintainv1.MediaScope_MEDIA_SCOPE_EPISODE:
		return ScopeEpisode
	case maintainv1.MediaScope_MEDIA_SCOPE_MOVIE_FILE:
		return ScopeMovieFile
	default:
		return ""
	}
}

func toProtoScope(s MediaScope) maintainv1.MediaScope {
	switch s {
	case ScopeMovie:
		return maintainv1.MediaScope_MEDIA_SCOPE_MOVIE
	case ScopeSeries:
		return maintainv1.MediaScope_MEDIA_SCOPE_SERIES
	case ScopeSeason:
		return maintainv1.MediaScope_MEDIA_SCOPE_SEASON
	case ScopeEpisode:
		return maintainv1.MediaScope_MEDIA_SCOPE_EPISODE
	case ScopeMovieFile:
		return maintainv1.MediaScope_MEDIA_SCOPE_MOVIE_FILE
	default:
		return maintainv1.MediaScope_MEDIA_SCOPE_UNSPECIFIED
	}
}

func protoOutcome(o maintainv1.RuleOutcome) RuleOutcome {
	switch o {
	case maintainv1.RuleOutcome_RULE_OUTCOME_PROTECT:
		return OutcomeProtect
	default:
		return OutcomeCandidate
	}
}

func toProtoOutcome(o RuleOutcome) maintainv1.RuleOutcome {
	if o == OutcomeProtect {
		return maintainv1.RuleOutcome_RULE_OUTCOME_PROTECT
	}
	return maintainv1.RuleOutcome_RULE_OUTCOME_CANDIDATE
}

func protoAction(a maintainv1.ArrAction) ArrAction {
	switch a {
	case maintainv1.ArrAction_ARR_ACTION_UNMONITOR:
		return ActionUnmonitor
	case maintainv1.ArrAction_ARR_ACTION_UNMONITOR_ONLY:
		return ActionUnmonitorOnly
	case maintainv1.ArrAction_ARR_ACTION_REMOVE_IF_EMPTY:
		return ActionRemoveIfEmpty
	case maintainv1.ArrAction_ARR_ACTION_DO_NOTHING:
		return ActionDoNothing
	case maintainv1.ArrAction_ARR_ACTION_MOVE:
		return ActionMove
	case maintainv1.ArrAction_ARR_ACTION_CHANGE_QUALITY_PROFILE:
		return ActionChangeQualityProfile
	default:
		return ActionDelete
	}
}

func toProtoAction(a ArrAction) maintainv1.ArrAction {
	switch a {
	case ActionUnmonitor:
		return maintainv1.ArrAction_ARR_ACTION_UNMONITOR
	case ActionUnmonitorOnly:
		return maintainv1.ArrAction_ARR_ACTION_UNMONITOR_ONLY
	case ActionRemoveIfEmpty:
		return maintainv1.ArrAction_ARR_ACTION_REMOVE_IF_EMPTY
	case ActionDoNothing:
		return maintainv1.ArrAction_ARR_ACTION_DO_NOTHING
	case ActionMove:
		return maintainv1.ArrAction_ARR_ACTION_MOVE
	case ActionChangeQualityProfile:
		return maintainv1.ArrAction_ARR_ACTION_CHANGE_QUALITY_PROFILE
	default:
		return maintainv1.ArrAction_ARR_ACTION_DELETE
	}
}

func protoCandidateStatus(s maintainv1.CandidateStatus) CandidateStatus {
	switch s {
	case maintainv1.CandidateStatus_CANDIDATE_STATUS_LEAVING_SOON:
		return StatusLeavingSoon
	case maintainv1.CandidateStatus_CANDIDATE_STATUS_APPROVED:
		return StatusApproved
	case maintainv1.CandidateStatus_CANDIDATE_STATUS_POSTPONED:
		return StatusPostponed
	case maintainv1.CandidateStatus_CANDIDATE_STATUS_CANCELLED:
		return StatusCancelled
	case maintainv1.CandidateStatus_CANDIDATE_STATUS_COMPLETED:
		return StatusCompleted
	case maintainv1.CandidateStatus_CANDIDATE_STATUS_FAILED:
		return StatusFailed
	default:
		return StatusPending
	}
}

func toProtoCandidateStatus(s CandidateStatus) maintainv1.CandidateStatus {
	switch s {
	case StatusLeavingSoon:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_LEAVING_SOON
	case StatusApproved:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_APPROVED
	case StatusPostponed:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_POSTPONED
	case StatusCancelled:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_CANCELLED
	case StatusCompleted:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_COMPLETED
	case StatusFailed:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_FAILED
	default:
		return maintainv1.CandidateStatus_CANDIDATE_STATUS_PENDING
	}
}
