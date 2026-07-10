package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/YouthVoyager/relay/internal/engine"
	"github.com/YouthVoyager/relay/internal/store"
	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
)

type RunsHandler struct {
	Store *store.Store
	Engine *engine.Engine
}

// Routes 返回本模块的子路由,挂载点由 main 决定。
func (h *RunsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/{id}", h.get)
	r.Get("/{id}/events", h.listEvents)
	return r
}
func (h *RunsHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.Store.Queries.ListEventsByRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		slog.Error("list events failed", "error", err,
			"request_id", middleware.GetReqID(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if events == nil {
		events = []sqlcgen.Event{}
	}
	writeJSON(w, http.StatusOK, events)
}

type createRunRequest struct {
	Goal string `json:"goal"`
}

func (h *RunsHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Goal == "" {
		writeError(w, http.StatusBadRequest, "goal is required")
		return
	}
	if len(req.Goal) > 10000 {
		writeError(w, http.StatusBadRequest, "goal too long (max 10000 chars)")
		return
	}

	run, err := h.Store.Queries.CreateRun(r.Context(), sqlcgen.CreateRunParams{
		ID:   newID("run"),
		Goal: req.Goal,
	})
	if err != nil {
		slog.Error("create run failed", "error", err,
			"request_id", middleware.GetReqID(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	// 后台执行。用 context.Background():run 的生命周期远长于这次 HTTP 请求,
	// 绝不能挂在 r.Context() 上——请求一结束它就被取消了。
	go h.Engine.Execute(context.Background(), run.ID)

	writeJSON(w, http.StatusCreated, run)
}

func (h *RunsHandler) list(w http.ResponseWriter, r *http.Request) {
	runs, err := h.Store.Queries.ListRuns(r.Context(), 50)
	if err != nil {
		slog.Error("list runs failed", "error", err,
			"request_id", middleware.GetReqID(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if runs == nil {
		runs = []sqlcgen.Run{} // 空列表返回 [] 而不是 null
	}
	writeJSON(w, http.StatusOK, runs)
}

func (h *RunsHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	run, err := h.Store.Queries.GetRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		slog.Error("get run failed", "error", err,
			"request_id", middleware.GetReqID(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, run)
}

// newID 生成 "run_a1b2c3..." 形式的随机 ID。
func newID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}