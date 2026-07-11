package engine

// 事件类型常量。事件是不可变的历史,类型名一旦使用就不要改——
// 老数据里已经写着它了。
const (
	EventRunStarted   = "run_started"
	EventLLMCalled    = "llm_called"    // 一次 LLM 调用完成(含响应)
	EventToolStarted  = "tool_started"  // 危险工具即将执行(意图先落库,见孤悬检测)
	EventToolExecuted = "tool_executed" // 一次工具执行完成(含结果)
	EventRunFinished  = "run_finished"  // 终态:succeeded / failed
	EventCompaction = "compaction" // 上下文压缩:此事件之前的历史被摘要取代
	EventApprovalRequested = "approval_requested"
	EventApprovalDecided   = "approval_decided"
)
type ApprovalRequestedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
	Reason     string `json:"reason"` // 给审批人看的说明
}

type ApprovalDecidedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Approved   bool   `json:"approved"`
	DecidedBy  string `json:"decided_by,omitempty"`
}
type CompactionPayload struct {
	Summary    string `json:"summary"`
	ThroughSeq int32  `json:"through_seq"` // 摘要覆盖到哪个 seq(含)
	SavedTokens int   `json:"saved_tokens,omitempty"` // 观测用:这次压缩省了多少
}

type LLMCalledPayload struct {
	Content      string          `json:"content,omitempty"`
	ToolCalls    []ToolCallInfo  `json:"tool_calls,omitempty"`
	FinishReason string          `json:"finish_reason"`
	PromptTokens int             `json:"prompt_tokens"`
	OutputTokens int             `json:"output_tokens"`
}

type ToolCallInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolStartedPayload 是危险工具执行前落库的"意图指纹"。带上 name 和
// arguments,恢复时无需回溯 llm_called 事件就能发起重跑审批和重新执行。
type ToolStartedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments"`
}

type ToolExecutedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Result     string `json:"result"`
}

type RunFinishedPayload struct {
	Status string `json:"status"` // succeeded | failed
	Error  string `json:"error,omitempty"`
}