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
- 使用 shell 时优先选择幂等的命令写法(如 mkdir -p、覆盖式写入),避免累加式操作
- 任务完成后,直接用文字总结结果,不要再调用工具`

type Engine struct {
	Store    *store.Store
	LLM      *llm.Client
	Registry *tools.Registry
	CompactionThreshold int
	Bus *Bus
}

// Execute 执行一个 run 直到终态。设计为在独立 goroutine 中调用。
func (e *Engine) Execute(ctx context.Context, runID string) {
	log := slog.With("run_id", runID)

	err := e.run(ctx, log, runID)
	switch {
	case err == nil:
		e.finish(context.Background(), log, runID, "succeeded", "")
	case errors.Is(err, errSuspendForApproval):
		log.Info("run suspended, waiting for approval")
		_ = e.Store.Queries.UpdateRunStatus(context.Background(), sqlcgen.UpdateRunStatusParams{
			ID: runID, Status: "waiting_approval",
		})
	case errors.Is(err, context.Canceled):
		// 被打断。是用户取消还是进程挂起?查一下意图标记。
		if e.isCancelRequested(runID) {
			log.Info("run cancelled by user")
			e.finish(context.Background(), log, runID, "cancelled", "")
		} else {
			// 挂起:什么都不写,status 留在 running,下次启动被恢复器捞起
			log.Info("run suspended for shutdown, will resume on next start")
		}
	default:
		log.Error("run failed", "error", err)
		e.finish(context.Background(), log, runID, "failed", err.Error())
	}
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

	// 崩溃恢复:先清算"孤悬的 tool_started"——上次进程死在副作用执行
	// 中途,命令是否已生效未知。不盲目重放,转人工确认。
	if err := e.resolveOrphanedStarts(ctx, log, runID, &seq); err != nil {
		return err
	}


	// 主循环
	for step := 0; step < maxSteps; step++ {
		var lastToolSig string
		var repeatCount int
		if err := ctx.Err(); err != nil {
			return err // context.Canceled 会被 Execute 的 switch 接住
		}
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
		if resp.Usage.PromptTokens > e.CompactionThreshold{
			if err := e.compact(ctx, log, runID, &seq, msgs, resp.Usage.PromptTokens); err != nil {
				return err
			}
			// 注意:不需要手动"应用"压缩——下一拍 buildMessages 从事件流
			// 重建时自然会遇到 compaction 事件并采用新视图。机制统一,零特判。
		}

		// ④ 分派
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			return nil // 模型说完了,任务完成
		}
		decisions, pending, err := e.loadApprovalState(ctx, runID)
		if err != nil {
			return err
		}

		for _, tc := range choice.Message.ToolCalls {
			sig := tc.Function.Name + "|" + tc.Function.Arguments
			if sig == lastToolSig {
				repeatCount++
				if repeatCount >= 3 {
					return fmt.Errorf("loop detected: %q repeated %d times consecutively",
						tc.Function.Name, repeatCount+1)
				}
			} else {
				lastToolSig, repeatCount = sig, 0
			}
			tool, ok := e.Registry.Get(tc.Function.Name)

			// 危险工具:走审批流程
			if ok {
				if d, isDangerous := tool.(tools.Dangerous); isDangerous {
					decided, approved := decisions[tc.ID]
					switch {
					case !decided && !pending[tc.ID]:
						// 从未请求过:发起审批,挂起整个 run
						if err := e.append(ctx, runID, &seq, EventApprovalRequested, ApprovalRequestedPayload{
							ToolCallID: tc.ID, ToolName: tc.Function.Name,
							Arguments: tc.Function.Arguments,
							Reason:    d.ApprovalReason(tc.Function.Arguments),
						}); err != nil {
							return err
						}
						return errSuspendForApproval // 特殊信号,见下
					case !decided:
						// 请求过但还没决定(恢复重放时走到这):继续等
						return errSuspendForApproval
					case !approved:
						// 被拒绝:告诉模型,让它换路走——拒绝是信息,不是终点
						if err := e.append(ctx, runID, &seq, EventToolExecuted, ToolExecutedPayload{
							ToolCallID: tc.ID, Name: tc.Function.Name,
							Result: "此操作已被用户拒绝执行。请换一种不需要该操作的方式完成任务,或说明无法完成的原因。",
						}); err != nil {
							return err
						}
						continue
					}
					// approved == true:放行,落到下面正常执行

					// 执行前先落"意图指纹"。若在执行与写 tool_executed 之间
					// 崩溃,恢复时据此可知命令状态未知,转人工而非盲目重放。
					if err := e.append(ctx, runID, &seq, EventToolStarted, ToolStartedPayload{
						ToolCallID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
					}); err != nil {
						return err
					}
				}
			}

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
var errSuspendForApproval = errors.New("suspend: waiting for approval")

// loadApprovalState 从事件流重建审批状态:
// decisions[toolCallID] = 是否批准;pending 里的是已请求未决定的。
func (e *Engine) loadApprovalState(ctx context.Context, runID string) (map[string]bool, map[string]bool, error) {
	events, err := e.Store.Queries.ListEventsByRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	decisions := map[string]bool{}
	pending := map[string]bool{}
	for _, ev := range events {
		switch ev.Type {
		case EventApprovalRequested:
			var p ApprovalRequestedPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				pending[p.ToolCallID] = true
			}
		case EventApprovalDecided:
			var p ApprovalDecidedPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				decisions[p.ToolCallID] = p.Approved
				delete(pending, p.ToolCallID)
			}
		}
	}
	return decisions, pending, nil
}

// resolveOrphanedStarts 清算崩溃留下的账:有 tool_started 却没有对应的
// tool_executed,说明上次进程死在副作用执行中途——命令可能已生效,也可能
// 没有。不盲目重放,复用审批机制把决定权交给人:批准=重跑一次,拒绝=跳过。
func (e *Engine) resolveOrphanedStarts(ctx context.Context, log *slog.Logger, runID string, seq *int32) error {
	events, err := e.Store.Queries.ListEventsByRun(ctx, runID)
	if err != nil {
		return err
	}

	// 单遍按序扫描。tool_started 会重置该 ID 的审批追踪,所以下面统计到的
	// requested/decided 天然只算 started 之后那一轮(即重跑审批),
	// 不会误读执行前的原始审批。
	type orphan struct {
		name, args         string
		requested, decided bool
		approved           bool
	}
	open := map[string]*orphan{}
	var order []string
	for _, ev := range events {
		switch ev.Type {
		case EventToolStarted:
			var p ToolStartedPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				if open[p.ToolCallID] == nil {
					order = append(order, p.ToolCallID)
				}
				open[p.ToolCallID] = &orphan{name: p.Name, args: p.Arguments}
			}
		case EventToolExecuted:
			var p ToolExecutedPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				delete(open, p.ToolCallID)
			}
		case EventApprovalRequested:
			var p ApprovalRequestedPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				if o := open[p.ToolCallID]; o != nil {
					o.requested = true
				}
			}
		case EventApprovalDecided:
			var p ApprovalDecidedPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				if o := open[p.ToolCallID]; o != nil {
					o.decided, o.approved = true, p.Approved
				}
			}
		}
	}

	for _, id := range order {
		o, ok := open[id]
		if !ok {
			continue
		}
		switch {
		case !o.requested:
			// 刚发现孤悬:发起重跑审批,挂起
			log.Warn("orphaned tool_started, asking human whether to re-run",
				"tool_call_id", id, "tool", o.name)
			if err := e.append(ctx, runID, seq, EventApprovalRequested, ApprovalRequestedPayload{
				ToolCallID: id, ToolName: o.name, Arguments: o.args,
				Reason: "上次进程在执行此命令期间中断,命令可能已生效但结果未知。批准=重新执行一次;拒绝=跳过重放,让模型自行核实。",
			}); err != nil {
				return err
			}
			return errSuspendForApproval
		case !o.decided:
			// 请求过还没决定(恢复重放时走到这):继续等
			return errSuspendForApproval
		case o.approved:
			log.Info("re-running orphaned tool call after approval",
				"tool_call_id", id, "tool", o.name)
			// 重跑本身也是一次副作用,同样先落指纹再执行
			if err := e.append(ctx, runID, seq, EventToolStarted, ToolStartedPayload{
				ToolCallID: id, Name: o.name, Arguments: o.args,
			}); err != nil {
				return err
			}
			result := e.executeTool(ctx, log, llm.ToolCall{
				ID: id, Type: "function",
				Function: llm.FunctionCall{Name: o.name, Arguments: o.args},
			})
			if err := e.append(ctx, runID, seq, EventToolExecuted, ToolExecutedPayload{
				ToolCallID: id, Name: o.name, Result: result,
			}); err != nil {
				return err
			}
		default:
			// 拒绝重跑:把"状态未知"这个事实告诉模型,让它核实后再走
			if err := e.append(ctx, runID, seq, EventToolExecuted, ToolExecutedPayload{
				ToolCallID: id, Name: o.name,
				Result: "[进程曾在执行此命令期间中断,命令是否已生效未知;用户选择不重新执行。请先用只读命令核实实际状态,再决定后续步骤。]",
			}); err != nil {
				return err
			}
		}
		// 已清算完毕。order 里可能有重复 ID(started→executed→再 started),
		// 删掉防止同一项被处理两次。
		delete(open, id)
	}
	return nil
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
		case EventCompaction:
			var pl CompactionPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return nil, fmt.Errorf("bad payload at seq %d: %w", ev.Seq, err)
			}
			// 重置对话:system + 原始目标 + 摘要,之前累积的全部丢弃
			msgs = []llm.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: run.Goal},
				{Role: "assistant", Content: "我已经执行了一段时间,以下是目前为止的执行摘要:\n\n" + pl.Summary},
				{Role: "user", Content: "请基于以上进展继续完成任务。"},
			}
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
				case EventToolStarted:
			// 对话层面无事发生:意图指纹是引擎的账本,不是模型对话的一部分
		case EventApprovalRequested:
			// 对话层面无事发生(审批是引擎与人的事,不是模型对话的一部分)
		case EventApprovalDecided:
			// 同上;拒绝的后果已通过 tool_executed 事件进入对话

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
	ev, err := e.Store.Queries.AppendEvent(ctx, sqlcgen.AppendEventParams{
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
		if e.Bus != nil {
			e.Bus.Publish(ev)
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

func (e *Engine) isCancelRequested(runID string) bool {
	status, err := e.Store.Queries.GetRunStatus(context.Background(), runID)
	return err == nil && status == "cancelling"
}