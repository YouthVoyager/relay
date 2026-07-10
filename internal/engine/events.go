package engine

// 事件类型常量。事件是不可变的历史,类型名一旦使用就不要改——
// 老数据里已经写着它了。
const (
	EventRunStarted   = "run_started"
	EventLLMCalled    = "llm_called"    // 一次 LLM 调用完成(含响应)
	EventToolExecuted = "tool_executed" // 一次工具执行完成(含结果)
	EventRunFinished  = "run_finished"  // 终态:succeeded / failed
)

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

type ToolExecutedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Result     string `json:"result"`
}

type RunFinishedPayload struct {
	Status string `json:"status"` // succeeded | failed
	Error  string `json:"error,omitempty"`
}