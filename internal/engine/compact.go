package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/YouthVoyager/relay/internal/llm"
)

const summarizerPrompt = `你是任务执行记录的归档员。下面是一个 agent 执行任务的对话记录。
请写一份供 agent 继续工作用的执行摘要,必须包含:
1. 任务目标是什么
2. 已经完成了哪些步骤,分别得到了什么关键结果(文件名、数据、结论等具体信息要保留)
3. 当前正在进行什么、下一步计划是什么
4. 遇到过哪些错误、是如何解决的(避免 agent 重蹈覆辙)
只输出摘要本身,不要任何前言或客套。信息要具体,宁可细节多也不要空泛。`

// compact 把当前对话历史压缩为一条 compaction 事件。
func (e *Engine) compact(ctx context.Context, log *slog.Logger, runID string, seq *int32, msgs []llm.Message, lastPromptTokens int) error {
	log.Info("compacting context", "prompt_tokens", lastPromptTokens, "messages", len(msgs))

	// 把对话拼成给归档员看的文本
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			continue // system prompt 不需要进摘要,重建时它总在
		}
		sb.WriteString("[")
		sb.WriteString(m.Role)
		sb.WriteString("] ")
		sb.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&sb, " <调用 %s(%s)>", tc.Function.Name, tc.Function.Arguments)
		}
		sb.WriteString("\n")
	}

	// 注意:压缩请求不带 tools——这是一次纯总结任务(3.1 讲过的动态工具集)
	resp, err := llm.WithRetry(ctx, llm.DefaultRetry, "llm.compact",
		func() (*llm.ChatResponse, error) {
			return e.LLM.Chat(ctx, llm.ChatRequest{
				Messages: []llm.Message{
					{Role: "system", Content: summarizerPrompt},
					{Role: "user", Content: sb.String()},
				},
				MaxTokens: 2000,
			})
		})
	if err != nil {
		return fmt.Errorf("compaction llm call: %w", err)
	}

	return e.append(ctx, runID, seq, EventCompaction, CompactionPayload{
		Summary:     resp.Choices[0].Message.Content,
		ThroughSeq:  *seq, // 到目前为止的一切都被覆盖
		SavedTokens: lastPromptTokens,
	})
}