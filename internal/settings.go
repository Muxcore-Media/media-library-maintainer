package internal

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Muxcore-Media/core/pkg/contracts"
)

func (m *Module) Settings() []contracts.SettingDef {
	return m.settingsDefs()
}

func (m *Module) UpdateSetting(key, value string) error {
	return m.updateSetting(key, value)
}

func (m *Module) settingsDefs() []contracts.SettingDef {
	return []contracts.SettingDef{
		{
			Key:         "scan_interval_minutes",
			Label:       "Scan Interval (minutes)",
			Type:        contracts.SettingTypeString,
			Value:       fmt.Sprintf("%d", int(m.getScanInterval().Minutes())),
			Description: "How often to evaluate rules and refresh candidates",
			Group:       "Scheduler",
		},
		{
			Key:         "act_interval_minutes",
			Label:       "Action Interval (minutes)",
			Type:        contracts.SettingTypeString,
			Value:       fmt.Sprintf("%d", int(m.getActInterval().Minutes())),
			Description: "How often to execute approved/grace-expired candidates",
			Group:       "Scheduler",
		},
		{
			Key:         "auto_act_enabled",
			Label:       "Auto Act Pending Candidates",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getAutoActEnabled()),
			Description: "When true, pending candidates act after act_after without manual approval",
			Group:       "Safety",
		},
		{
			Key:         "dry_run",
			Label:       "Dry Run",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getDryRun()),
			Description: "Scan and act runs log matches but do not persist candidates or delete files",
			Group:       "Safety",
		},
		{
			Key:         "max_actions_per_run",
			Label:       "Max Actions Per Run",
			Type:        contracts.SettingTypeString,
			Value:       strconv.Itoa(m.getMaxActionsPerRun()),
			Description: "Batch limit for destructive actions each act cycle",
			Group:       "Safety",
		},
		{
			Key:         "leaving_soon_notify_enabled",
			Label:       "Leaving Soon Notifications",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getLeavingSoonNotifyEnabled()),
			Description: "Notify when items enter leaving-soon staging (requires notification module)",
			Group:       "Notifications",
		},
		{
			Key:         "overlay_enabled",
			Label:       "Poster Overlays",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getOverlayEnabled()),
			Description: "Apply leaving-soon banner overlay to posters via MediaAdmin",
			Group:       "Actions",
		},
		{
			Key:         "notify_enabled",
			Label:       "Notify On Runs",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getNotifyEnabled()),
			Description: "Send notifications after scan/act runs via notification module",
			Group:       "Notifications",
		},
		{
			Key:         "add_list_exclusion_on_delete",
			Label:       "Add List Exclusion On Delete",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getAddListExclusionOnDelete()),
			Description: "Record TMDB exclusions on delete to prevent re-import (Radarr API when RADARR_URL set)",
			Group:       "Actions",
		},
		{
			Key:         "move_path",
			Label:       "Move Archive Path",
			Type:        contracts.SettingTypeString,
			Value:       m.getMovePath(),
			Description: "Destination for move-instead-of-delete actions (MAINTAINER_MOVE_PATH)",
			Group:       "Actions",
		},
		{
			Key:         "disk_act_max_free_percent",
			Label:       "Disk Act Max Free %",
			Type:        contracts.SettingTypeString,
			Value:       fmt.Sprintf("%.1f", m.getDiskActMaxFreePercent()),
			Description: "Only act when free disk % is at or below this value (0 = disabled)",
			Group:       "Actions",
		},
		{
			Key:         "free_up_root_path",
			Label:       "Free-Up Root Path",
			Type:        contracts.SettingTypeString,
			Value:       m.getFreeUpRootPath(),
			Description: "Path monitored for emergency free-up target (default /data)",
			Group:       "Actions",
		},
		{
			Key:         "userdata_dir",
			Label:       "Userdata Directory",
			Type:        contracts.SettingTypeString,
			Value:       m.userdataDir(),
			Description: "Path to userdata-local store for watch-based rules (MAINTAINER_USERDATA_DIR)",
			Group:       "Watch History",
		},
		{
			Key:         "overlay_show_date",
			Label:       "Overlay Date Pill",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getOverlayShowDate()),
			Description: "Show days-until-delete pill on leaving-soon poster overlays",
			Group:       "Actions",
		},
		{
			Key:         "overlay_date_format",
			Label:       "Overlay Date Format",
			Type:        contracts.SettingTypeString,
			Value:       m.overlayStyle().DateFormat,
			Description: "Go time format for overlay date pill when days mode is off (e.g. Jan 2)",
			Group:       "Actions",
		},
		{
			Key:         "overlay_bar_color",
			Label:       "Overlay Bar Color",
			Type:        contracts.SettingTypeString,
			Value:       m.overlayStyle().BarColorString(),
			Description: "Bottom banner color as #RRGGBB or R,G,B[,A]",
			Group:       "Actions",
		},
		{
			Key:         "overlay_pill_color",
			Label:       "Overlay Pill Color",
			Type:        contracts.SettingTypeString,
			Value:       m.overlayStyle().PillColorString(),
			Description: "Date pill background color",
			Group:       "Actions",
		},
		{
			Key:         "overlay_pill_text_color",
			Label:       "Overlay Pill Text Color",
			Type:        contracts.SettingTypeString,
			Value:       m.overlayStyle().PillTextColorString(),
			Description: "Date pill text color",
			Group:       "Actions",
		},
		{
			Key:         "overlay_title_card_enabled",
			Label:       "Title Card Overlays",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getOverlayTitleCardEnabled()),
			Description: "Apply leaving-soon banner to backdrop/title-card artwork (Maintainerr-style)",
			Group:       "Actions",
		},
		{
			Key:         "overlay_template_json",
			Label:       "Poster Overlay Template JSON",
			Type:        contracts.SettingTypeString,
			Value:       m.getOverlayTemplateJSON(),
			Description: "Maintainerr-style overlay template for posters (elements: shape, variable, text, image)",
			Group:       "Actions",
		},
		{
			Key:         "overlay_titlecard_template_json",
			Label:       "Title Card Overlay Template JSON",
			Type:        contracts.SettingTypeString,
			Value:       m.getOverlayTitleCardTemplateJSON(),
			Description: "Overlay template JSON for backdrop/title-card artwork",
			Group:       "Actions",
		},
		{
			Key:         "protect_unwatched_requesters",
			Label:       "Protect Unwatched Requesters",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getProtectUnwatchedRequestersEnabled()),
			Description: "Auto-protect requested media until the requester watches it (userdata-local)",
			Group:       "Safety",
		},
		{
			Key:         "protect_request_min_age_days",
			Label:       "Request Protection Min Age (days)",
			Type:        contracts.SettingTypeString,
			Value:       strconv.Itoa(m.getProtectRequestMinAgeDays()),
			Description: "Always protect requested media younger than this many days",
			Group:       "Safety",
		},
		{
			Key:         "protect_request_max_days",
			Label:       "Request Protection Max Age (days)",
			Type:        contracts.SettingTypeString,
			Value:       strconv.Itoa(m.getProtectRequestMaxDays()),
			Description: "Stop protecting unwatched requesters after this many days (0 = no limit)",
			Group:       "Safety",
		},
		{
			Key:         "download_client_url",
			Label:       "qBittorrent URL",
			Type:        contracts.SettingTypeString,
			Value:       m.getDownloadClientURL(),
			Description: "qBittorrent WebUI base URL for torrent cleanup after delete (QBITTORRENT_URL)",
			Group:       "Download Client",
		},
		{
			Key:         "download_client_username",
			Label:       "qBittorrent Username",
			Type:        contracts.SettingTypeString,
			Value:       m.getDownloadClientUsername(),
			Description: "qBittorrent WebUI username",
			Group:       "Download Client",
		},
		{
			Key:         "download_client_password",
			Label:       "qBittorrent Password",
			Type:        contracts.SettingTypeString,
			Value:       "********",
			Description: "qBittorrent WebUI password",
			Group:       "Download Client",
		},
		{
			Key:         "download_client_delete_data",
			Label:       "Delete Torrent Data",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(m.getDownloadClientDeleteData()),
			Description: "Remove downloaded files from disk when removing torrents (respects cross-seed protection)",
			Group:       "Download Client",
		},
		{
			Key:         "download_client_fallback_ratio",
			Label:       "Fallback Seed Ratio",
			Type:        contracts.SettingTypeString,
			Value:       fmt.Sprintf("%.2f", m.getDownloadClientFallbackRatio()),
			Description: "Minimum ratio before removing torrents when qBittorrent has no seed limit",
			Group:       "Download Client",
		},
	}
}

func (m *Module) updateSetting(key, value string) error { //nolint:gocyclo // settings surface maps many module toggles
	value = strings.TrimSpace(value)
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	switch key {
	case "scan_interval_minutes":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid scan_interval_minutes %q", value)
		}
		m.scanInterval = time.Duration(n) * time.Minute
		return nil
	case "act_interval_minutes":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid act_interval_minutes %q", value)
		}
		m.actInterval = time.Duration(n) * time.Minute
		return nil
	case "auto_act_enabled":
		m.autoActEnabled = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "dry_run":
		m.dryRun = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "max_actions_per_run":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid max_actions_per_run %q", value)
		}
		m.maxActionsPerRun = n
		return nil
	case "notify_enabled":
		m.notifyEnabled = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "leaving_soon_notify_enabled":
		m.leavingSoonNotifyEnabled = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "overlay_enabled":
		m.overlayEnabled = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "overlay_show_date":
		m.overlayShowDate = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "overlay_date_format":
		m.overlayDateFormat = value
		return nil
	case "overlay_bar_color":
		m.overlayBarColor = value
		return nil
	case "overlay_pill_color":
		m.overlayPillColor = value
		return nil
	case "overlay_pill_text_color":
		m.overlayPillTextColor = value
		return nil
	case "overlay_title_card_enabled":
		m.overlayTitleCardEnabled = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "overlay_template_json":
		m.overlayTemplateJSON = value
		return nil
	case "overlay_titlecard_template_json":
		m.overlayTitleCardTemplateJSON = value
		return nil
	case "protect_unwatched_requesters":
		m.protectUnwatchedRequesters = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "protect_request_min_age_days":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid protect_request_min_age_days %q", value)
		}
		m.protectRequestMinAgeDays = n
		return nil
	case "protect_request_max_days":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid protect_request_max_days %q", value)
		}
		m.protectRequestMaxDays = n
		return nil
	case "move_path":
		m.movePath = value
		return nil
	case "disk_act_max_free_percent":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("invalid disk_act_max_free_percent %q", value)
		}
		m.diskActMaxFreePercent = f
		return nil
	case "free_up_root_path":
		m.freeUpRootPath = value
		return nil
	case "add_list_exclusion_on_delete":
		m.addListExclusionOnDelete = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "userdata_dir":
		m.userdataDataDir = value
		return nil
	case "download_client_url":
		m.downloadClientURL = value
		return nil
	case "download_client_username":
		m.downloadClientUser = value
		return nil
	case "download_client_password":
		if value != "" && value != "********" {
			m.downloadClientPass = value
		}
		return nil
	case "download_client_delete_data":
		m.downloadClientDeleteDataSet = true
		m.downloadClientDeleteData = strings.EqualFold(value, "true") || value == "1"
		return nil
	case "download_client_fallback_ratio":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f < 0.5 {
			return fmt.Errorf("invalid download_client_fallback_ratio %q", value)
		}
		m.downloadClientFallbackRatio = f
		return nil
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
}

func (m *Module) getScanInterval() time.Duration {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.scanInterval > 0 {
		return m.scanInterval
	}
	return 24 * time.Hour
}

func (m *Module) getActInterval() time.Duration {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.actInterval > 0 {
		return m.actInterval
	}
	return 6 * time.Hour
}

func (m *Module) getAutoActEnabled() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.autoActEnabled
}

func (m *Module) getDryRun() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.dryRun
}

func (m *Module) getMaxActionsPerRun() int {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.maxActionsPerRun > 0 {
		return m.maxActionsPerRun
	}
	return 50
}

func (m *Module) getMovePath() string {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.movePath != "" {
		return m.movePath
	}
	if v := os.Getenv("MAINTAINER_MOVE_PATH"); v != "" {
		return v
	}
	return ""
}

func (m *Module) getDiskActMaxFreePercent() float64 {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.diskActMaxFreePercent
}

func (m *Module) getFreeUpRootPath() string {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.freeUpRootPath != "" {
		return m.freeUpRootPath
	}
	if v := os.Getenv("MAINTAINER_FREE_UP_ROOT"); v != "" {
		return v
	}
	return "/data"
}

func (m *Module) getAddListExclusionOnDelete() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.addListExclusionOnDelete {
		return true
	}
	return strings.EqualFold(os.Getenv("MAINTAINER_ADD_LIST_EXCLUSION"), "true")
}
