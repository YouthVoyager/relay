package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/YouthVoyager/relay/internal/llm"
)

// scriptedLLM 按预设剧本依次返回响应——一个完全受测试控制的"模型"。
type scriptedLLM struct {
	mu    sync.Mutex
	steps []*llm.ChatResponse
	calls int
}

func (s *scriptedLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.steps) {
		return nil, fmt.Errorf("script exhausted: call %d but only %d steps", s.calls+1, len(s.steps))
	}
	resp := s.steps[s.calls]
	s.calls++
	return resp, nil
}

func (s *scriptedLLM) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// 剧本构造助手
func stepToolCall(id, name, args string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.Message{
				Role: "assistant", Content: "我来执行这一步。",
				ToolCalls: []llm.ToolCall{{
					ID: id, Type: "function",
					Function: llm.FunctionCall{Name: name, Arguments: args},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 20},
	}
}

func stepStop(content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message:      llm.Message{Role: "assistant", Content: content},
			FinishReason: "stop",
		}},
		Usage: llm.Usage{PromptTokens: 120, CompletionTokens: 30},
	}
}