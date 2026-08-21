package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

func (m *Module) ListExclusionLists(ctx context.Context, req *maintainv1.ListExclusionListsRequest) (*maintainv1.ListExclusionListsResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return &maintainv1.ListExclusionListsResponse{}, nil
	}
	rows, err := m.db.Query(`SELECT id, name, type, list_url, api_key, tmdb_ids_json, last_synced, created_at FROM exclusion_lists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lists []*maintainv1.ExclusionList
	for rows.Next() {
		l, err := scanExclusionListRow(rows)
		if err != nil {
			continue
		}
		lists = append(lists, l)
	}
	return &maintainv1.ListExclusionListsResponse{Lists: lists}, nil
}

func (m *Module) UpsertExclusionList(ctx context.Context, req *maintainv1.UpsertExclusionListRequest) (*maintainv1.UpsertExclusionListResponse, error) {
	in := req.GetList()
	if in == nil {
		return nil, fmt.Errorf("list required")
	}
	id := in.GetId()
	if id == "" {
		id = newID("excl")
	}
	now := nowRFC()
	tmdbJSON := "[]"
	if len(in.GetTmdbIds()) > 0 {
		var ids []int
		for _, id := range in.GetTmdbIds() {
			ids = append(ids, int(id))
		}
		raw, _ := json.Marshal(ids)
		tmdbJSON = string(raw)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`INSERT INTO exclusion_lists (id, name, type, list_url, api_key, tmdb_ids_json, last_synced, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, type=excluded.type, list_url=excluded.list_url,
			api_key=excluded.api_key, tmdb_ids_json=CASE WHEN excluded.tmdb_ids_json != '[]' THEN excluded.tmdb_ids_json ELSE exclusion_lists.tmdb_ids_json END`,
		id, in.GetName(), in.GetType(), in.GetListUrl(), in.GetApiKey(), tmdbJSON, in.GetLastSynced(), now)
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`SELECT id, name, type, list_url, api_key, tmdb_ids_json, last_synced, created_at FROM exclusion_lists WHERE id = ?`, id)
	l, err := scanExclusionListRow(row)
	if err != nil {
		return nil, err
	}
	return &maintainv1.UpsertExclusionListResponse{List: l}, nil
}

func (m *Module) DeleteExclusionList(ctx context.Context, req *maintainv1.DeleteExclusionListRequest) (*maintainv1.DeleteExclusionListResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.Exec(`DELETE FROM exclusion_lists WHERE id = ?`, req.GetId())
	return &maintainv1.DeleteExclusionListResponse{}, err
}

func (m *Module) SyncExclusionLists(ctx context.Context, req *maintainv1.SyncExclusionListsRequest) (*maintainv1.SyncExclusionListsResponse, error) {
	synced, loaded, err := m.syncExclusionLists(ctx)
	if err != nil {
		return nil, err
	}
	return &maintainv1.SyncExclusionListsResponse{ListsSynced: int32(synced), IdsLoaded: int32(loaded)}, nil
}

func (m *Module) ExportRules(ctx context.Context, req *maintainv1.ExportRulesRequest) (*maintainv1.ExportRulesResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return &maintainv1.ExportRulesResponse{RulesJson: "[]"}, nil
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
	raw, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	yamlOut, _ := rulesToYAML(rules)
	return &maintainv1.ExportRulesResponse{RulesJson: string(raw), RulesYaml: yamlOut}, nil
}

func (m *Module) ImportRules(ctx context.Context, req *maintainv1.ImportRulesRequest) (*maintainv1.ImportRulesResponse, error) {
	var rules []*maintainv1.RuleGroup
	var err error
	if y := strings.TrimSpace(req.GetRulesYaml()); y != "" {
		rules, err = parseRulesYAML(y)
	} else {
		err = json.Unmarshal([]byte(req.GetRulesJson()), &rules)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid rules: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if req.GetReplace() {
		if _, err := m.db.Exec(`DELETE FROM rule_groups`); err != nil {
			return nil, err
		}
	}
	imported := 0
	now := nowRFC()
	for _, in := range rules {
		if in == nil {
			continue
		}
		id := in.GetId()
		if id == "" {
			id = newID("rule")
		}
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
		imported++
	}
	return &maintainv1.ImportRulesResponse{Imported: int32(imported)}, nil
}

func scanExclusionListRow(s scanner) (*maintainv1.ExclusionList, error) {
	var id, name, typ, url, key, tmdbJSON, synced, created string
	if err := s.Scan(&id, &name, &typ, &url, &key, &tmdbJSON, &synced, &created); err != nil {
		return nil, err
	}
	return &maintainv1.ExclusionList{
		Id: id, Name: name, Type: typ, ListUrl: url, ApiKey: key,
		TmdbIds: parseTMDBIDList(tmdbJSON), LastSynced: synced, CreatedAt: created,
	}, nil
}
