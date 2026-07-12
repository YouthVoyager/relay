package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireToken 校验 Authorization: Bearer <token>。token 为空则放行(开发模式)。
func RequireToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			got, ok := strings.CutPrefix(auth, "Bearer ")
			// 常数时间比较:防时序侧信道猜 token
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}