package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/YouthVoyager/relay/internal/engine"
	"github.com/YouthVoyager/relay/internal/store"
	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
)

type StreamHandler struct {
	Store *store.Store
	Bus   *engine.Bus
}

func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")

	// SSE 必须能逐条刷出——检查底层支持 Flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// 断线重连:浏览器自动带 Last-Event-ID(全局事件 id)
	var afterID int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		afterID, _ = strconv.ParseInt(v, 10, 64)
	}

	// ① 先订阅,再查历史——顺序是防丢事件的关键(见下文)
	ch, unsubscribe := h.Bus.Subscribe(runID, 64)
	defer unsubscribe()

	// ② 补发历史(> afterID 的部分)
	events, err := h.Store.Queries.ListEventsAfter(r.Context(), sqlcgen.ListEventsAfterParams{
		RunID: runID, ID: afterID,
	})
	if err != nil {
		return
	}
	lastSent := afterID
	for _, ev := range events {
		writeSSE(w, ev)
		lastSent = ev.ID
	}
	flusher.Flush()

	// ③ 持续推增量 + 心跳
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case ev := <-ch:
			if ev.ID <= lastSent {
				continue // 订阅与查历史的重叠区,去重
			}
			writeSSE(w, ev)
			lastSent = ev.ID
			flusher.Flush()
		case <-heartbeat.C:
			// 注释行心跳:防中间设备掐断空闲连接,客户端会忽略它
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return // 客户端断开,defer 退订
		}
	}
}

func writeSSE(w http.ResponseWriter, ev sqlcgen.Event) {
	data, _ := json.Marshal(ev)
	fmt.Fprintf(w, "id: %d\nevent: run_event\ndata: %s\n\n", ev.ID, data)
}