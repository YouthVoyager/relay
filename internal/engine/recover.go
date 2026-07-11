package engine

import (
	"context"
	"log/slog"
)

// RecoverOrphans 找出上次进程死亡时被遗弃的 run,重新执行。
// 在服务启动时调用一次。
func (e *Engine) ListOrphans(ctx context.Context)([]string,error) {
	orphans, err := e.Store.Queries.ListRunsByStatus(ctx, "running")
	if err != nil {
		return []string{},err
	}
	if len(orphans) == 0 {
		slog.Info("no orphan runs to recover")
		return []string{},nil
	}
	runIDs := []string{}
	slog.Info("recovering orphan runs", "count", len(orphans))
	for _, run := range orphans {
		slog.Info("resuming run", "run_id", run.ID)
		runIDs = append(runIDs, run.ID)
	}
	return runIDs,nil
}
