package internal

import (
	"context"
	"log/slog"
	"time"
)

func (m *Module) schedulerLoop(ctx context.Context) { //nolint:gosec // scheduled maintenance loop intentionally runs until module stop
	time.Sleep(15 * time.Second)
	var lastScan, lastAct time.Time

	for { //nolint:gosec // scheduler intentionally runs until module shutdown
		select {
		case <-ctx.Done():
			return
		default:
		}
		now := time.Now()
		scanDue := now.Sub(lastScan) >= m.getScanInterval()
		actDue := now.Sub(lastAct) >= m.getActInterval()

		if scanDue { //nolint:gosec // scheduled scan work is intentionally uncancelled within the loop tick
			runID := m.startRun(ctx, "scan_scheduled", m.getDryRun())
			found, err := m.runScan(ctx, m.getDryRun())
			status := "completed"
			errMsg := ""
			if err != nil {
				status = "failed"
				errMsg = err.Error()
				slog.Warn("scheduled scan failed", "error", err)
			} else {
				slog.Info("scheduled scan complete", "candidates", found)
			}
			m.finishRun(ctx, runID, status, found, 0, 0, errMsg)
			m.notifyRun(ctx, "scan_scheduled", found, 0, 0, m.getDryRun(), errMsg)
			lastScan = now
		}

		if actDue {
			runID := m.startRun(ctx, "act_scheduled", m.getDryRun())
			taken, failed, err := m.runAct(ctx, actOptions{dryRun: m.getDryRun(), maxActions: m.getMaxActionsPerRun()})
			status := "completed"
			errMsg := ""
			if err != nil {
				status = "failed"
				errMsg = err.Error()
				slog.Warn("scheduled act failed", "error", err)
			} else if taken > 0 || failed > 0 {
				slog.Info("scheduled act complete", "taken", taken, "failed", failed)
			}
			m.finishRun(ctx, runID, status, 0, taken, failed, errMsg)
			m.notifyRun(ctx, "act_scheduled", 0, taken, failed, m.getDryRun(), errMsg)
			lastAct = now
		}

		sleep := 1 * time.Minute
		if !scanDue && !actDue {
			nextScan := m.getScanInterval() - now.Sub(lastScan)
			nextAct := m.getActInterval() - now.Sub(lastAct)
			if nextScan < sleep {
				sleep = nextScan
			}
			if nextAct < sleep {
				sleep = nextAct
			}
		}
		if sleep < 30*time.Second {
			sleep = 30 * time.Second
		}
		time.Sleep(sleep)
	}
}
