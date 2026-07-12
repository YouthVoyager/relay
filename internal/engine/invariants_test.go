package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/YouthVoyager/relay/internal/store"
	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
)

// testStore 连接测试数据库;未配置则跳过(单元测试环境不强求 DB)。
func testStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	st, err := store.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(st.Close)
	// 专用测试库,每个测试从白纸开始——固定 run ID 才能可重复执行
	if _, err := st.Pool.Exec(context.Background(), "TRUNCATE events, runs"); err != nil {
		t.Fatalf("truncate test db: %v", err)
	}
	return st
}

// CheckRunInvariants 校验一个 run 的事件流必须满足的全部性质。
// 它被设计为可复用:任何测试在任何操作后都可以调用它做"体检"。
func CheckRunInvariants(t *testing.T, st *store.Store, runID string) {
	t.Helper()
	ctx := context.Background()

	events, err := st.Queries.ListEventsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	run, err := st.Queries.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	// 不变量 1:seq 从 1 开始严格连续,无空洞无重复
	for i, ev := range events {
		if ev.Seq != int32(i+1) {
			t.Errorf("invariant violated: seq gap at index %d, got seq=%d", i, ev.Seq)
		}
	}

	// 不变量 2:每个 payload 都是合法 JSON
	for _, ev := range events {
		if !json.Valid(ev.Payload) {
			t.Errorf("invariant violated: invalid payload at seq %d", ev.Seq)
		}
	}

	// 不变量 3:runs 投影与事件流一致——
	// 终态 run 必须有且仅有一个 run_finished,且状态匹配;
	// 非终态 run 必须没有 run_finished
	finished := 0
	var finalStatus string
	for _, ev := range events {
		if ev.Type == EventRunFinished {
			finished++
			var p RunFinishedPayload
			_ = json.Unmarshal(ev.Payload, &p)
			finalStatus = p.Status
		}
	}
	terminal := run.Status == "succeeded" || run.Status == "failed" || run.Status == "cancelled"
	switch {
	case terminal && finished != 1:
		t.Errorf("invariant violated: terminal run has %d run_finished events", finished)
	case terminal && run.Status != "cancelled" && finalStatus != run.Status:
		t.Errorf("invariant violated: run.status=%s but run_finished.status=%s", run.Status, finalStatus)
	case !terminal && finished != 0:
		t.Errorf("invariant violated: non-terminal run has run_finished event")
	}

	// 不变量 4:每个 tool_executed 都能配对到此前某个 llm_called 里的 tool_call
	known := map[string]bool{}
	for _, ev := range events {
		switch ev.Type {
		case EventLLMCalled:
			var p LLMCalledPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				for _, tc := range p.ToolCalls {
					known[tc.ID] = true
				}
			}
		case EventToolExecuted:
			var p ToolExecutedPayload
			if json.Unmarshal(ev.Payload, &p) == nil && !known[p.ToolCallID] {
				t.Errorf("invariant violated: tool_executed at seq %d references unknown tool_call %q",
					ev.Seq, p.ToolCallID)
			}
		}
	}
}

// 先用一个手工构造的 run 验证体检器本身能跑
func TestInvariants_ManualRun(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	run, err := st.Queries.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID: "test_inv_" + t.Name(), Goal: "invariant smoke test",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	seq := int32(0)
	e := &Engine{Store: st}
	for _, typ := range []string{EventRunStarted} {
		if err := e.append(ctx, run.ID, &seq, typ, nil); err != nil {
			t.Fatal(err)
		}
	}
	e.finish(ctx, testLogger(), run.ID, "succeeded", "")

	CheckRunInvariants(t, st, run.ID)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}