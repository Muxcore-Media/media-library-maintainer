package internal

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func (m *Module) notifyLeavingSoon(ctx context.Context, ec EvalContext, actAfter, label string) {
	if !m.getLeavingSoonNotifyEnabled() {
		return
	}
	if m.alreadyNotifiedLeavingSoon(ec.Scope, ec.ItemID) {
		return
	}
	title := "Leaving soon: " + ec.Title
	when := actAfter
	if t, err := time.Parse(time.RFC3339, actAfter); err == nil {
		when = t.Format("Jan 2, 2006")
	}
	msg := fmt.Sprintf("%s (%d) scheduled for action on %s. Collection: %s", ec.Title, ec.Year, when, label)
	m.postNotification(ctx, title, msg, "info", map[string]string{
		"kind":      "leaving_soon",
		"scope":     string(ec.Scope),
		"item_id":   ec.ItemID,
		"title":     ec.Title,
		"act_after": actAfter,
		"label":     label,
	})
	m.markLeavingSoonNotified(ec.Scope, ec.ItemID)
}

func (m *Module) alreadyNotifiedLeavingSoon(scope MediaScope, itemID string) bool {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return false
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM leaving_soon_notified WHERE scope = ? AND item_id = ?`, scope, itemID).Scan(&n)
	return n > 0
}

func (m *Module) markLeavingSoonNotified(scope MediaScope, itemID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.Exec(`INSERT INTO leaving_soon_notified (scope, item_id, notified_at) VALUES (?, ?, ?)
		ON CONFLICT(scope, item_id) DO UPDATE SET notified_at=excluded.notified_at`, scope, itemID, nowRFC())
}

func (m *Module) getLeavingSoonNotifyEnabled() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.leavingSoonNotifyEnabled {
		return true
	}
	if strings.EqualFold(os.Getenv("MAINTAINER_LEAVING_SOON_NOTIFY"), "false") {
		return false
	}
	return m.notifyEnabled
}

func (m *Module) getOverlayEnabled() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.overlayEnabled {
		return true
	}
	return strings.EqualFold(os.Getenv("MAINTAINER_OVERLAY_ENABLED"), "true")
}

func (m *Module) getOverlayTitleCardEnabled() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.overlayTitleCardEnabled {
		return true
	}
	return strings.EqualFold(os.Getenv("MAINTAINER_OVERLAY_TITLE_CARD"), "true")
}

func (m *Module) getOverlayTemplateJSON() string {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.overlayTemplateJSON != "" {
		return m.overlayTemplateJSON
	}
	return defaultPosterOverlayTemplate
}

func (m *Module) getOverlayTitleCardTemplateJSON() string {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.overlayTitleCardTemplateJSON != "" {
		return m.overlayTitleCardTemplateJSON
	}
	return defaultTitleCardOverlayTemplate
}

func (m *Module) getOverlayShowDate() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.overlayShowDate {
		return true
	}
	return !strings.EqualFold(os.Getenv("MAINTAINER_OVERLAY_SHOW_DATE"), "false")
}

func (m *Module) getProtectUnwatchedRequestersEnabled() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.protectUnwatchedRequesters {
		return true
	}
	return strings.EqualFold(os.Getenv("MAINTAINER_PROTECT_UNWATCHED_REQUESTERS"), "true")
}

func (m *Module) getProtectRequestMinAgeDays() int {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.protectRequestMinAgeDays
}

func (m *Module) getProtectRequestMaxDays() int {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.protectRequestMaxDays
}
