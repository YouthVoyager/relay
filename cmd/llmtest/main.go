package main

import (
	"context"
	// "encoding/json"
	"fmt"
	"os"

	"github.com/YouthVoyager/relay/internal/config"
	"github.com/YouthVoyager/relay/internal/llm"
	"github.com/YouthVoyager/relay/internal/tools"
)

func main() {
	cfg, err := config.Load()
	if err != nil || cfg.LLMBaseURL == "" || cfg.LLMAPIKey == "" || cfg.LLMModel == "" {
		fmt.Println("请设置 LLM_BASE_URL / LLM_API_KEY / LLM_MODEL")
		os.Exit(1)
	}
	client := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	ctx := context.Background()

	// 测试 1:普通对话
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "用一句话介绍你自己"}},
	})
	if err != nil {
		fmt.Println("测试1失败:", err)
		os.Exit(1)
	}
	fmt.Println("== 测试1 普通对话 ==")
	fmt.Println("finish_reason:", resp.Choices[0].FinishReason)
	fmt.Println("content:", resp.Choices[0].Message.Content)
	fmt.Printf("usage: %+v\n\n", resp.Usage)

	// 测试 2:模型意图 → 本地真实执行
	ws, _ := os.Getwd()
	reg := tools.NewRegistry()
	reg.Register(&tools.ListDir{Workspace: ws})
	reg.Register(&tools.ReadFile{Workspace: ws})

	resp2, err := client.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "看看当前目录下有哪些文件,然后读一下 go.mod 的内容"},
		},
		Tools: reg.Specs(),
	})
	if err != nil {
		fmt.Println("测试2失败:", err)
		os.Exit(1)
	}
	fmt.Println("== 测试2 意图→执行 ==")
	fmt.Println("finish_reason:", resp2.Choices[0].FinishReason)
	for _, tc := range resp2.Choices[0].Message.ToolCalls {
		fmt.Printf("模型意图: %s(%s)\n", tc.Function.Name, tc.Function.Arguments)

		tool, ok := reg.Get(tc.Function.Name)
		if !ok {
			fmt.Println("  -> 模型调用了不存在的工具!")
			continue
		}
		result, err := tool.Execute(ctx, tc.Function.Arguments)
		if err != nil {
			fmt.Println("  -> 工具故障:", err)
			continue
		}
		fmt.Printf("  -> 执行结果(前200字符): %.200s\n", result)
	}

	// 测试 3:沙箱逃逸防御
	rf, _ := reg.Get("read_file")
	out, _ := rf.Execute(ctx, `{"path": "../../../etc/passwd"}`)
	fmt.Println("\n== 测试3 逃逸防御 ==")
	fmt.Println(out) // 期待看到 escapes workspace
}