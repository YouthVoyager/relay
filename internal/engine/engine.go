package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/YouthVoyager/relay/internal/llm"
	"github.com/YouthVoyager/relay/internal/store"
	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
	"github.com/YouthVoyager/relay/internal/tools"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxSteps        = 30 // 每个 run 最多多少拍,防失控的最粗一道闸
	defaultMaxToken = 4096
)

const systemPrompt = `你是 Relay,一个严谨的任务执行助手。
你通过调用工具来完成用户交给你的任务。
- 一步一步来,每一步只做一件事
- 工具返回错误时,分析原因并调整策略,不要原样重试
- 任务完成后,直接用文字总结结果,不要再调用工具`

type Engine struct {
	Store    *store.Store
	LLM      *llm.Client
	Registry *tools.Registry
}

// Execute 执行一个 run 直到终态。设计为在独立 goroutine 中调用。
func (e *Engine) Execute(ctx context.Context, runID string) {
	log := slog.With("run_id", runID)

	if err := e.run(ctx, log, runID); err != nil {
		log.Error("run failed", "error", err)
		e.finish(ctx, log, runID, "failed", err.Error())
		return
	}
	e.finish(ctx, log, runID, "succeeded", "")
}

func (e *Engine) run(ctx context.Context, log *slog.Logger, runID string) error {
	q := e.Store.Queries

	run, err := q.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run: %w", err)
	}

	if err := q.UpdateRunStatus(ctx, sqlcgen.UpdateRunStatusParams{
		ID: runID, Status: "running",
	}); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}

	seq, err := q.GetLastSeq(ctx, runID)
	if err != nil {
		return fmt.Errorf("get last seq: %w", err)
	}

	if seq == 0 {
		if err := e.append(ctx, runID, &seq, EventRunStarted, nil); err != nil {
			return err
		}
	}

	// 主循环
	for step := 0; step < maxSteps; step++ {
		// ① 从事件流重建对话——不维护内存态,这是可恢复性的根
		msgs, err := e.buildMessages(ctx, run)
		if err != nil {
			return err
		}

		// ② 调 LLM
		resp, err := llm.WithRetry(ctx, llm.DefaultRetry, "llm.chat",
			func() (*llm.ChatResponse, error) {
				return e.LLM.Chat(ctx, llm.ChatRequest{
					Messages:  msgs,
					Tools:     e.Registry.Specs(),
					MaxTokens: defaultMaxToken,
				})
			})
		if err != nil {
			return fmt.Errorf("llm call: %w", err)
		}
		choice := resp.Choices[0]

		// ③ 记录 LLM 事件
		pl := LLMCalledPayload{
			Content:      choice.Message.Content,
			FinishReason: choice.FinishReason,
			PromptTokens: resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
		for _, tc := range choice.Message.ToolCalls {
			pl.ToolCalls = append(pl.ToolCalls, ToolCallInfo{
				ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
			})
		}
		if err := e.append(ctx, runID, &seq, EventLLMCalled, pl); err != nil {
			return err
		}
		log.Info("llm step", "step", step, "finish_reason", choice.FinishReason,
			"tool_calls", len(choice.Message.ToolCalls),
			"prompt_tokens", resp.Usage.PromptTokens)

		// ④ 分派
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			return nil // 模型说完了,任务完成
		}

		for _, tc := range choice.Message.ToolCalls {
			result := e.executeTool(ctx, log, tc)
			if err := e.append(ctx, runID, &seq, EventToolExecuted, ToolExecutedPayload{
				ToolCallID: tc.ID, Name: tc.Function.Name, Result: result,
			}); err != nil {
				return err
			}
		}
	}

	return fmt.Errorf("reached max steps (%d) without finishing", maxSteps)
}

// executeTool 执行单个工具调用,任何失败都转成给模型看的文本。
func (e *Engine) executeTool(ctx context.Context, log *slog.Logger, tc llm.ToolCall) string {
	tool, ok := e.Registry.Get(tc.Function.Name)
	if !ok {
		// 模型幻觉出了不存在的工具——告诉它,让它自己纠正
		return fmt.Sprintf("error: tool %q does not exist", tc.Function.Name)
	}
	result, err := tool.Execute(ctx, tc.Function.Arguments)
	if err != nil {
		log.Error("tool system failure", "tool", tc.Function.Name, "error", err)
		return fmt.Sprintf("error: tool %q failed internally: %v", tc.Function.Name, err)
	}
	return result
}

// buildMessages 从事件流重建 LLM 对话历史。事件是真相,消息是投影。
func (e *Engine) buildMessages(ctx context.Context, run sqlcgen.Run) ([]llm.Message, error) {
	events, err := e.Store.Queries.ListEventsByRun(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	msgs := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: run.Goal},
	}

	for _, ev := range events {
		switch ev.Type {
		case EventLLMCalled:
			var pl LLMCalledPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return nil, fmt.Errorf("bad payload at seq %d: %w", ev.Seq, err)
			}
			m := llm.Message{Role: "assistant", Content: pl.Content}
			for _, tc := range pl.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, llm.ToolCall{
					ID: tc.ID, Type: "function",
					Function: llm.FunctionCall{Name: tc.Name, Arguments: tc.Arguments},
				})
			}
			msgs = append(msgs, m)
		case EventToolExecuted:
			var pl ToolExecutedPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return nil, fmt.Errorf("bad payload at seq %d: %w", ev.Seq, err)
			}
			msgs = append(msgs, llm.Message{
				Role: "tool", Content: pl.Result, ToolCallID: pl.ToolCallID,
			})
		}
	}
	return msgs, nil
}

// append 写入一个事件并推进序号。
func (e *Engine) append(ctx context.Context, runID string, seq *int32, typ string, payload any) error {
	data := []byte("{}")
	if payload != nil {
		var err error
		if data, err = json.Marshal(payload); err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
	}
	*seq++
	// if _, err := e.Store.Queries.AppendEvent(ctx, sqlcgen.AppendEventParams{
	// 	RunID: runID, Seq: *seq, Type: typ, Payload: data,
	// }); err != nil {
	// 	return fmt.Errorf("append event seq=%d: %w", *seq, err)
	// }
	// return nil
	_, err := e.Store.Queries.AppendEvent(ctx, sqlcgen.AppendEventParams{
		RunID: runID, Seq: *seq, Type: typ, Payload: data,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			slog.Warn("event already exists, treating as success",
				"run_id", runID, "seq", *seq, "type", typ)
			return nil
		}
		return fmt.Errorf("append event seq=%d: %w", *seq, err)
	}
	return nil
}

func (e *Engine) finish(ctx context.Context, log *slog.Logger, runID, status, errMsg string) {
	seq, err := e.Store.Queries.GetLastSeq(ctx, runID)
	if err != nil {
		log.Error("finish: get seq failed", "error", err)
		return
	}
	_ = e.append(ctx, runID, &seq, EventRunFinished, RunFinishedPayload{Status: status, Error: errMsg})

	var ep *string
	if errMsg != "" {
		ep = &errMsg
	}
	if err := e.Store.Queries.UpdateRunStatus(ctx, sqlcgen.UpdateRunStatusParams{
		ID: runID, Status: status, Error: ep,
	}); err != nil {
		log.Error("finish: update status failed", "error", err)
	}
}