package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YouthVoyager/relay/internal/llm"
)

// resolveInWorkspace 把模型给的相对路径解析到工作区内,并防止逃逸。
func resolveInWorkspace(workspace, p string) (string, error) {
	// 拼接后规范化:filepath.Clean 会折叠 ".." 和 "."
	abs := filepath.Clean(filepath.Join(workspace, p))

	// 规范化后必须仍在工作区内,否则就是逃逸企图
	if abs != workspace && !strings.HasPrefix(abs, workspace+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes workspace", p)
	}
	return abs, nil
}

// ---- read_file ----

type ReadFile struct{ Workspace string }

func (t *ReadFile) Spec() llm.FunctionSpec {
	return llm.FunctionSpec{
		Name:        "read_file",
		Description: "读取工作区内一个文本文件的内容。路径相对于工作区根目录。",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "相对路径,如 notes/todo.md"}
			},
			"required": ["path"]
		}`),
	}
}

func (t *ReadFile) Execute(ctx context.Context, args string) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), nil
	}

	abs, err := resolveInWorkspace(t.Workspace, in.Path)
	if err != nil {
		return err.Error(), nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("read failed: %v", err), nil
	}

	const maxLen = 50_000
	if len(data) > maxLen {
		return string(data[:maxLen]) + fmt.Sprintf("\n...[truncated, total %d bytes]", len(data)), nil
	}
	return string(data), nil
}

// ---- list_dir ----

type ListDir struct{ Workspace string }

func (t *ListDir) Spec() llm.FunctionSpec {
	return llm.FunctionSpec{
		Name:        "list_dir",
		Description: "列出工作区内一个目录的条目。路径相对于工作区根目录,用 \".\" 表示根目录。",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "相对路径,默认 \".\""}
			}
		}`),
	}
}

func (t *ListDir) Execute(ctx context.Context, args string) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if args != "" {
		if err := json.Unmarshal([]byte(args), &in); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), nil
		}
	}
	if in.Path == "" {
		in.Path = "."
	}

	abs, err := resolveInWorkspace(t.Workspace, in.Path)
	if err != nil {
		return err.Error(), nil
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("list failed: %v", err), nil
	}

	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			sb.WriteString(e.Name() + "/\n")
		} else {
			sb.WriteString(e.Name() + "\n")
		}
	}
	if sb.Len() == 0 {
		return "(empty directory)", nil
	}
	return sb.String(), nil
}