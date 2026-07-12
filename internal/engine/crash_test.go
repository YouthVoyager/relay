package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YouthVoyager/relay/internal/llm"
	"github.com/YouthVoyager/relay/internal/store"
	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
	"github.com/YouthVoyager/relay/internal/tools"
)


var errInjectedCrash = errors.New("injected crash")

func TestCrashRecovery_ResumesWithoutDuplication(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	run, err := st.Queries.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID: "test_crash_" + t.Name(), Goal: "crash recovery test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 剧本:第 1 次调用 → 要求 list_dir;第 2 次调用 → 宣布完成
	script := &scriptedLLM{steps: []*llm.ChatResponse{
		stepToolCall("call_1", "list_dir", `{"path":"."}`),
		stepStop("任务完成。"),
	}}

	reg := tools.NewRegistry()
	reg.Register(&tools.ListDir{Workspace: t.TempDir()})

	// CompactionThreshold 必须显式给大:零值意味着每拍都触发压缩,
	// 压缩的摘要调用会吃掉剧本步数
	eng := &Engine{Store: st, LLM: script, Registry: reg, CompactionThreshold: 1 << 30}

	// ===== 第一幕:执行到 seq=3(tool_executed 刚落库)时"崩溃" =====
	eng.afterAppend = func(typ string, seq int32) error {
		if seq == 3 {
			return errInjectedCrash
		}
		return nil
	}

	err = eng.run(ctx, testLogger(), run.ID)
	if !errors.Is(err, errInjectedCrash) {
		t.Fatalf("expected injected crash, got: %v", err)
	}

	// 崩溃后的世界:status 停在 running(孤儿),事件停在 seq=3
	r, _ := st.Queries.GetRun(ctx, run.ID)
	if r.Status != "running" {
		t.Fatalf("after crash status = %s, want running", r.Status)
	}
	CheckRunInvariants(t, st, run.ID) // 半途的 run 也必须满足全部不变量!
	callsBeforeRecovery := script.callCount()

	// ===== 第二幕:模拟重启恢复——撤掉炸弹,重新 Execute =====
	eng.afterAppend = nil
	eng.Execute(ctx, run.ID)

	r, _ = st.Queries.GetRun(ctx, run.ID)
	if r.Status != "succeeded" {
		t.Fatalf("after recovery status = %s (error=%v), want succeeded", r.Status, r.Error)
	}
	CheckRunInvariants(t, st, run.ID)

	// 事件序列必须是精确的 5 步:started, llm, tool, llm, finished——
	// 不变量 1 已保证无空洞,这里再断言总数,确保恢复没有多写
	events, _ := st.Queries.ListEventsByRun(ctx, run.ID)
	if len(events) != 5 {
		t.Fatalf("expected exactly 5 events, got %d", len(events))
	}

	// 记录 at-least-once 语义:恢复后 LLM 被再次调用(重建视图后的第 2 步),
	// 这是设计内的行为——多花一次调用,历史零污染
	if script.callCount() != callsBeforeRecovery+1 {
		t.Fatalf("expected exactly 1 more llm call after recovery, got %d -> %d",
			callsBeforeRecovery, script.callCount())
	}
}

// 审批通过后的恢复必须由引擎重放上一拍未分派的 tool_calls,而不是重新调
// LLM 靠模型重发——真实 API 会生成新 tool_call_id,旧审批对不上号。
func TestApprovalResume_ReplaysWithoutNewLLMCall(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	run, err := st.Queries.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID: "test_replay_" + t.Name(), Goal: "approval replay test",
	})
	if err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	script := &scriptedLLM{steps: []*llm.ChatResponse{
		stepToolCall("call_1", "run_shell", `{"command":"echo x >> counter.txt"}`),
		stepStop("任务完成。"),
	}}
	reg := tools.NewRegistry()
	reg.Register(&tools.RunShell{Workspace: ws, Timeout: 5 * time.Second})
	eng := &Engine{Store: st, LLM: script, Registry: reg, CompactionThreshold: 1 << 30}

	// 危险工具 → 挂起等审批
	if err := eng.run(ctx, testLogger(), run.ID); !errors.Is(err, errSuspendForApproval) {
		t.Fatalf("expected suspend for approval, got: %v", err)
	}
	approve(t, st, run.ID, "call_1", true)

	// 恢复:重放阶段直接执行已批准的调用,主循环只多一次"收尾"调用
	eng.Execute(ctx, run.ID)

	r, _ := st.Queries.GetRun(ctx, run.ID)
	if r.Status != "succeeded" {
		t.Fatalf("status = %s (error=%v), want succeeded", r.Status, r.Error)
	}
	CheckRunInvariants(t, st, run.ID)

	if got := script.callCount(); got != 2 {
		t.Fatalf("expected exactly 2 llm calls (原调用+收尾), got %d", got)
	}
	data, err := os.ReadFile(filepath.Join(ws, "counter.txt"))
	if err != nil || string(data) != "x\n" {
		t.Fatalf("command should have run exactly once, counter.txt = %q (err=%v)", data, err)
	}
	// 审批只发起过一轮——不存在"新 ID 对不上号、循环要审批"
	events, _ := st.Queries.ListEventsByRun(ctx, run.ID)
	requests := 0
	for _, ev := range events {
		if ev.Type == EventApprovalRequested {
			requests++
		}
	}
	if requests != 1 {
		t.Fatalf("expected exactly 1 approval_requested, got %d", requests)
	}
}

// 情形 B 的清算:危险工具执行中途崩溃 → tool_started 孤悬 → 恢复时
// 不盲目重放,转人工审批;批准后重跑恰好一次。
func TestCrashRecovery_OrphanedToolStarted_ApprovedRerun(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	run, err := st.Queries.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID: "test_orphan_" + t.Name(), Goal: "orphaned start test",
	})
	if err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	// 故意用非幂等命令:执行次数直接刻在文件行数上
	shellArgs := `{"command":"echo x >> counter.txt"}`
	// 只有两步:恢复靠引擎重放上一拍的 tool_calls,不需要模型重发
	script := &scriptedLLM{steps: []*llm.ChatResponse{
		stepToolCall("call_1", "run_shell", shellArgs),
		stepStop("任务完成。"),
	}}

	reg := tools.NewRegistry()
	reg.Register(&tools.RunShell{Workspace: ws, Timeout: 5 * time.Second})
	// CompactionThreshold 必须显式给大:零值意味着每拍都触发压缩,
	// 压缩的摘要调用会吃掉剧本步数
	eng := &Engine{Store: st, LLM: script, Registry: reg, CompactionThreshold: 1 << 30}

	// ===== 第一幕:危险工具触发审批,挂起 =====
	if err := eng.run(ctx, testLogger(), run.ID); !errors.Is(err, errSuspendForApproval) {
		t.Fatalf("expected suspend for approval, got: %v", err)
	}
	approve(t, st, run.ID, "call_1", true)

	// ===== 第二幕:批准后继续,在 tool_started 落库后、执行前"崩溃" =====
	eng.afterAppend = func(typ string, seq int32) error {
		if typ == EventToolStarted {
			return errInjectedCrash
		}
		return nil
	}
	if err := eng.run(ctx, testLogger(), run.ID); !errors.Is(err, errInjectedCrash) {
		t.Fatalf("expected injected crash, got: %v", err)
	}
	CheckRunInvariants(t, st, run.ID)

	// ===== 第三幕:重启恢复——发现孤悬 started,应转人工而不是直接重跑 =====
	eng.afterAppend = nil
	eng.Execute(ctx, run.ID)

	r, _ := st.Queries.GetRun(ctx, run.ID)
	if r.Status != "waiting_approval" {
		t.Fatalf("after recovery status = %s, want waiting_approval", r.Status)
	}
	if data, err := os.ReadFile(filepath.Join(ws, "counter.txt")); err == nil {
		t.Fatalf("command must not have re-run before approval, but counter.txt = %q", data)
	}
	CheckRunInvariants(t, st, run.ID)

	// ===== 第四幕:人批准重跑 → 执行恰好一次,任务走到终点 =====
	approve(t, st, run.ID, "call_1", true)
	eng.Execute(ctx, run.ID)

	r, _ = st.Queries.GetRun(ctx, run.ID)
	if r.Status != "succeeded" {
		t.Fatalf("final status = %s (error=%v), want succeeded", r.Status, r.Error)
	}
	CheckRunInvariants(t, st, run.ID)

	data, err := os.ReadFile(filepath.Join(ws, "counter.txt"))
	if err != nil {
		t.Fatalf("counter.txt should exist after approved rerun: %v", err)
	}
	if string(data) != "x\n" {
		t.Fatalf("command should have run exactly once, counter.txt = %q", data)
	}
}

// 同上,但人拒绝重跑:命令不执行,"状态未知"作为工具结果告知模型,任务继续。
func TestCrashRecovery_OrphanedToolStarted_RejectedRerun(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	run, err := st.Queries.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID: "test_orphrej_" + t.Name(), Goal: "orphaned start rejected test",
	})
	if err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	shellArgs := `{"command":"echo x >> counter.txt"}`
	script := &scriptedLLM{steps: []*llm.ChatResponse{
		stepToolCall("call_1", "run_shell", shellArgs),
		stepStop("已根据现状收尾。"),
	}}

	reg := tools.NewRegistry()
	reg.Register(&tools.RunShell{Workspace: ws, Timeout: 5 * time.Second})
	// CompactionThreshold 必须显式给大:零值意味着每拍都触发压缩,
	// 压缩的摘要调用会吃掉剧本步数
	eng := &Engine{Store: st, LLM: script, Registry: reg, CompactionThreshold: 1 << 30}

	if err := eng.run(ctx, testLogger(), run.ID); !errors.Is(err, errSuspendForApproval) {
		t.Fatalf("expected suspend for approval, got: %v", err)
	}
	approve(t, st, run.ID, "call_1", true)

	eng.afterAppend = func(typ string, seq int32) error {
		if typ == EventToolStarted {
			return errInjectedCrash
		}
		return nil
	}
	if err := eng.run(ctx, testLogger(), run.ID); !errors.Is(err, errInjectedCrash) {
		t.Fatalf("expected injected crash, got: %v", err)
	}

	eng.afterAppend = nil
	eng.Execute(ctx, run.ID)
	if r, _ := st.Queries.GetRun(ctx, run.ID); r.Status != "waiting_approval" {
		t.Fatalf("after recovery status = %s, want waiting_approval", r.Status)
	}

	// 拒绝重跑
	approve(t, st, run.ID, "call_1", false)
	eng.Execute(ctx, run.ID)

	r, _ := st.Queries.GetRun(ctx, run.ID)
	if r.Status != "succeeded" {
		t.Fatalf("final status = %s (error=%v), want succeeded", r.Status, r.Error)
	}
	CheckRunInvariants(t, st, run.ID)

	if _, err := os.ReadFile(filepath.Join(ws, "counter.txt")); err == nil {
		t.Fatal("command must not have run after rejection")
	}
	// 模型必须收到"状态未知"的工具结果,而不是凭空少一条消息
	events, _ := st.Queries.ListEventsByRun(ctx, run.ID)
	found := false
	for _, ev := range events {
		if ev.Type == EventToolExecuted {
			var p ToolExecutedPayload
			if json.Unmarshal(ev.Payload, &p) == nil && strings.Contains(p.Result, "未知") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected a tool_executed event telling the model the outcome is unknown")
	}
}

// approve 模拟审批 API:直接追加 approval_decided 事件(与 handler 同款写法)。
func approve(t *testing.T, st *store.Store, runID, toolCallID string, approved bool) {
	t.Helper()
	ctx := context.Background()
	seq, err := st.Queries.GetLastSeq(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(ApprovalDecidedPayload{ToolCallID: toolCallID, Approved: approved})
	if _, err := st.Queries.AppendEvent(ctx, sqlcgen.AppendEventParams{
		RunID: runID, Seq: seq + 1, Type: EventApprovalDecided, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAppendIdempotent_DuplicateSeqIsSuccess(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	run, err := st.Queries.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID: "test_idem_" + t.Name(), Goal: "idempotency test",
	})
	if err != nil {
		t.Fatal(err)
	}
	eng := &Engine{Store: st}

	// 模拟 4.1 的情形 C:事件已落库,但引擎"以为"自己还没写
	seq := int32(0)
	if err := eng.append(ctx, run.ID, &seq, EventRunStarted, nil); err != nil {
		t.Fatal(err)
	}

	// 引擎"失忆"后重写同一个 seq——必须被视为成功
	seqReplay := int32(0)
	if err := eng.append(ctx, run.ID, &seqReplay, EventRunStarted, nil); err != nil {
		t.Fatalf("duplicate append should be treated as success, got: %v", err)
	}

	events, _ := st.Queries.ListEventsByRun(ctx, run.ID)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event, got %d", len(events))
	}
	CheckRunInvariants(t, st, run.ID)
}