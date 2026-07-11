package engine

import (
	"context"
	"log/slog"
	"sync"
)

// Runner 跟踪所有活跃 run 的生命周期:启动、取消、优雅挂起。
type Runner struct {
	engine *Engine

	mu     sync.Mutex
	active map[string]context.CancelFunc // runID -> 取消其执行的函数
	wg     sync.WaitGroup                // 等待所有 run 退出
	closed bool
}

func NewRunner(e *Engine) *Runner {
	return &Runner{engine: e, active: make(map[string]context.CancelFunc)}
}

// Start 在后台执行一个 run。返回 false 表示 Runner 已关闭(拒绝新任务)。
func (r *Runner) Start(runID string) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.active[runID] = cancel
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.active, runID)
			r.mu.Unlock()
			r.wg.Done()
		}()
		r.engine.Execute(ctx, runID)
	}()
	return true
}

// Cancel 取消一个正在执行的 run。返回 false 表示它不在活跃列表。
func (r *Runner) Cancel(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[runID]
	if ok {
		cancel()
	}
	return ok
}

// Shutdown 停止接收新 run,打断所有活跃 run,等它们退出(或 ctx 超时)。
func (r *Runner) Shutdown(ctx context.Context) {
	r.mu.Lock()
	r.closed = true
	n := len(r.active)
	for _, cancel := range r.active {
		cancel()
	}
	r.mu.Unlock()

	slog.Info("runner draining", "active_runs", n)
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("all runs exited cleanly")
	case <-ctx.Done():
		slog.Warn("shutdown timeout, some runs may not have flushed")
	}
}