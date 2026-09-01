package internal

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func (m *Module) persistSetting(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(context.Background(), `INSERT INTO settings_kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
}

func (m *Module) loadPersistedSettings(ctx context.Context) { //nolint:gocyclo // restores all mesh settings at startup
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	rows, err := m.db.QueryContext(ctx, `SELECT key, value FROM settings_kv`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		m.applyPersistedSetting(k, v)
	}
}

func (m *Module) applyPersistedSetting(key, value string) { //nolint:gocyclo // mirrors updateSetting keys
	switch key {
	case "scan_interval_minutes":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			m.scanInterval = time.Duration(n) * time.Minute
		}
	case "act_interval_minutes":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			m.actInterval = time.Duration(n) * time.Minute
		}
	case "auto_act_enabled":
		m.autoActEnabled = parseBoolSetting(value)
	case "dry_run":
		m.dryRun = parseBoolSetting(value)
	case "max_actions_per_run":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			m.maxActionsPerRun = n
		}
	case "notify_enabled":
		m.notifyEnabled = parseBoolSetting(value)
	case "leaving_soon_notify_enabled":
		m.leavingSoonNotifyEnabled = parseBoolSetting(value)
	case "overlay_enabled":
		m.overlayEnabled = parseBoolSetting(value)
	case "overlay_show_date":
		m.overlayShowDate = parseBoolSetting(value)
	case "overlay_date_format":
		m.overlayDateFormat = value
	case "overlay_bar_color":
		m.overlayBarColor = value
	case "overlay_pill_color":
		m.overlayPillColor = value
	case "overlay_pill_text_color":
		m.overlayPillTextColor = value
	case "overlay_title_card_enabled":
		m.overlayTitleCardEnabled = parseBoolSetting(value)
	case "overlay_template_json":
		m.overlayTemplateJSON = value
	case "overlay_titlecard_template_json":
		m.overlayTitleCardTemplateJSON = value
	case "protect_unwatched_requesters":
		m.protectUnwatchedRequesters = parseBoolSetting(value)
	case "protect_request_min_age_days":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			m.protectRequestMinAgeDays = n
		}
	case "protect_request_max_days":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			m.protectRequestMaxDays = n
		}
	case "move_path":
		m.movePath = value
	case "disk_act_max_free_percent":
		if f, err := strconv.ParseFloat(value, 64); err == nil && f >= 0 {
			m.diskActMaxFreePercent = f
		}
	case "free_up_root_path":
		m.freeUpRootPath = value
	case "add_list_exclusion_on_delete":
		m.addListExclusionOnDelete = parseBoolSetting(value)
	case "userdata_dir":
		m.userdataDataDir = value
	case "download_client_url":
		m.downloadClientURL = value
	case "download_client_username":
		m.downloadClientUser = value
	case "download_client_password":
		if v := strings.TrimSpace(value); v != "" && v != "********" {
			m.downloadClientPass = v
		}
	case "download_client_delete_data":
		m.downloadClientDeleteDataSet = true
		m.downloadClientDeleteData = parseBoolSetting(value)
	case "download_client_fallback_ratio":
		if f, err := strconv.ParseFloat(value, 64); err == nil && f >= 0.5 {
			m.downloadClientFallbackRatio = f
		}
	}
}

func parseBoolSetting(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
