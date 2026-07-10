package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// 模拟不稳定的 LLM 服务:每 3 次请求,前 2 次返回 429,第 3 次返回一个固定回复。
func main() {
	var n atomic.Int64
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		c := n.Add(1)
		if c%3 != 0 {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"任务完成。"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	})
	fmt.Println("flaky llm on :9090")
	http.ListenAndServe(":9090", nil)
}