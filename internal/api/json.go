package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json failed", "error", err)
	}
}

// writeError 返回统一结构的错误响应:{"error": "..."}
// 注意:msg 是给客户端看的,永远不要把内部 err.Error() 直接放进去。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)) // 上限 1MB
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}