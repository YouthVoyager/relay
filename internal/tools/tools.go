package tools

import (
	"context"
	// "encoding/json"
	"fmt"

	"github.com/YouthVoyager/relay/internal/llm"
)

// Tool 是所有工具的统一接口:一份给模型看的声明 + 一段引擎可执行的实现。
type Tool interface {
	// Spec 返回工具的声明(名字、描述、参数 schema),会进入每次 LLM 请求。
	Spec() llm.FunctionSpec
	// Execute 执行工具。args 是模型生成的 arguments 原始 JSON 字符串。
	// 返回给模型看的结果文本。error 表示工具自身故障(区别于"执行了但结果不理想")。
	Execute(ctx context.Context, args string) (string, error)
}

// Registry 管理一组工具,提供查找和批量导出声明。
type Registry struct {
	tools map[string]Tool
	order []string // 保持注册顺序,让 Specs() 输出稳定
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	name := t.Spec().Name
	if _, exists := r.tools[name]; exists {
		// 注册期冲突是编程错误,不是运行时错误——直接 panic,fail fast
		panic(fmt.Sprintf("tool %q registered twice", name))
	}
	r.tools[name] = t
	r.order = append(r.order, name)
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs 导出所有工具声明,供构建 ChatRequest.Tools。
func (r *Registry) Specs() []llm.Tool {
	out := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, llm.Tool{
			Type:     "function",
			Function: r.tools[name].Spec(),
		})
	}
	return out
}