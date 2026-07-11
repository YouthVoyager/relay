package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/YouthVoyager/relay/internal/api"
	"github.com/YouthVoyager/relay/internal/config"
	"github.com/YouthVoyager/relay/internal/engine"
	"github.com/YouthVoyager/relay/internal/llm"
	"github.com/YouthVoyager/relay/internal/store"
	"github.com/YouthVoyager/relay/internal/tools"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	slog.Info("config loaded", "addr", cfg.Addr, "DatabaseURL", cfg.DatabaseURL)
	st, err := store.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("store init failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database connected")
	if cfg.LLMBaseURL == "" || cfg.LLMAPIKey == "" || cfg.LLMModel == "" {
		slog.Error("LLM_BASE_URL / LLM_API_KEY / LLM_MODEL must be set")
		os.Exit(1)
	}
	r := chi.NewRouter()

	// chi 官方中间件:注意顺序,从外到内依次生效
	r.Use(middleware.RequestID) // 给每个请求生成唯一 ID,注入 context
	r.Use(api.RequestLogger) //记录每个请求的方法、路径、状态码、字节数、耗时和 request ID
	r.Use(middleware.Recoverer) // handler 里 panic 时兜底,返回 500 而不是让进程崩掉
	
	ws := filepath.Join(".", "workspace")
	os.MkdirAll(ws, 0o755)
	absWS, _ := filepath.Abs(ws)

	reg := tools.NewRegistry()
	reg.Register(&tools.ListDir{Workspace: absWS})
	reg.Register(&tools.ReadFile{Workspace: absWS})
	bus := engine.NewBus()
	eng := &engine.Engine{
		Store:    st,
		LLM:      llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel),
		Registry: reg,
		CompactionThreshold: cfg.CompactionThreshold,
		Bus: bus,
	}
	runner := engine.NewRunner(eng)
	orphansIDs,err := eng.ListOrphans(context.Background());
	if  err != nil {
		slog.Error("recover orphans failed", "error", err)
		os.Exit(1)
	}
	for _,id := range orphansIDs{
		runner.Start(id)
	}
	streamHeader := &api.StreamHandler{
		Store: st,
		Bus: bus,
	}
	runsHandler := &api.RunsHandler{Store: st, Runner: runner,StreamHandler: streamHeader}
	r.Mount("/api/runs", runsHandler.Routes())
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	// readiness:能对外服务吗?依赖(DB)必须就绪。
	// 负载均衡器用它决定"要不要把流量给这个实例"。
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := st.Pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unavailable","reason":"database"}`))
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: r,
	}

	go func() {
		slog.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	slog.Info("shutdown signal received, draining...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	runner.Shutdown(ctx) // 复用同一个 10 秒的关闭 ctx
	st.Close()
	slog.Info("database pool closed")
	slog.Info("server stopped cleanly")
}