package internal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	notificationv1 "github.com/Muxcore-Media/contracts-notification/muxcore/notification/v1"
)

func (m *Module) notifyRun(ctx context.Context, kind string, found, taken, failed int, dryRun bool, errMsg string) {
	if !m.getNotifyEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	severity := "info"
	title := "Library maintainer " + kind
	msg := fmt.Sprintf("found=%d taken=%d failed=%d dry_run=%v", found, taken, failed, dryRun)
	if errMsg != "" {
		severity = "warning"
		msg += " error=" + errMsg
	} else if failed > 0 {
		severity = "warning"
	} else if taken > 0 {
		severity = "success"
	}

	m.postNotification(ctx, title, msg, severity, map[string]string{
		"kind":    kind,
		"found":   fmt.Sprintf("%d", found),
		"taken":   fmt.Sprintf("%d", taken),
		"failed":  fmt.Sprintf("%d", failed),
		"dry_run": fmt.Sprintf("%v", dryRun),
	})
}

func (m *Module) postNotification(ctx context.Context, title, message, severity string, fields map[string]string) {
	addr, err := m.findCapabilityAddr(ctx, "notification")
	if err != nil {
		slog.Debug("maintainer: notification capability missing", "error", err)
		return
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	cli := notificationv1.NewNotificationServiceClient(conn)

	_, err = cli.Notify(ctx, &notificationv1.NotifyRequest{
		Title:        title,
		Message:      message,
		Severity:     parseNotifySeverity(severity),
		SourceModule: m.id,
		Fields:       fields,
	})
	if err != nil {
		slog.Debug("maintainer: notify failed", "error", err)
	}
}

func parseNotifySeverity(s string) notificationv1.Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success":
		return notificationv1.Severity_SEVERITY_SUCCESS
	case "warning":
		return notificationv1.Severity_SEVERITY_WARNING
	case "error":
		return notificationv1.Severity_SEVERITY_ERROR
	default:
		return notificationv1.Severity_SEVERITY_INFO
	}
}

func (m *Module) getNotifyEnabled() bool {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.notifyEnabled
}
