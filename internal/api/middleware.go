package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger 记录每个请求的方法、路径、状态码、字节数、耗时和 request ID。
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包装 ResponseWriter,以便事后读取状态码和写出的字节数
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// 把包装后的 ww(而不是原始 w)传给下游
		next.ServeHTTP(ww, r)

		// ---- 走到这里,下游 handler 已经执行完毕 ----
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", float64(time.Since(start).Microseconds())/1000.0,
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}