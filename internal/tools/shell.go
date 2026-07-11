package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/YouthVoyager/relay/internal/llm"
)

type RunShell struct {
	Workspace string
	Timeout   time.Duration // 单条命令的最长执行时间
}

func (t *RunShell) Spec() llm.FunctionSpec {
	return llm.FunctionSpec{
		Name: "run_shell",
		Description: "在工作区目录内执行一条 shell 命令,返回其输出。" +
			"适用于文件处理、数据转换等。命令有超时限制,输出会被截断。" +
			"每条命令都需要用户审批,请把多个相关操作合并成一条命令以减少打扰。",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "要执行的命令,如 grep -r TODO . | head -20"}
			},
			"required": ["command"]
		}`),
	}
}

func (t *RunShell) ApprovalReason(args string) string {
	var in struct{ Command string `json:"command"` }
	_ = json.Unmarshal([]byte(args), &in)
	return fmt.Sprintf("将在工作区执行 shell 命令:%s", in.Command)
}

func (t *RunShell) Execute(ctx context.Context, args string) (string, error) {
	var in struct{ Command string `json:"command"` }
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), nil
	}
	if in.Command == "" {
		return "error: command is empty", nil
	}

	// context 树第二层:工具级超时,挂在 run 级 ctx 之下。
	// 父(run)取消 → 子立即取消(株连);子超时 → 只影响这一次执行(不上溯)。
	cctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "sh", "-c", in.Command)
	cmd.Dir = t.Workspace
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + t.Workspace}

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond)

	out := formatShellResult(stdout.String(), stderr.String(), err, elapsed, cctx)
	return out, nil
}

const maxOutputLen = 20_000

func formatShellResult(stdout, stderr string, err error, elapsed time.Duration, ctx context.Context) string {
	var b bytes.Buffer

	switch {
	case ctx.Err() == context.DeadlineExceeded:
		fmt.Fprintf(&b, "[命令超时被终止,已运行 %s]\n", elapsed)
	case ctx.Err() == context.Canceled:
		fmt.Fprintf(&b, "[命令因任务取消被终止]\n")
	case err != nil:
		// 非零退出码等:对模型是重要信息,不是工具故障
		fmt.Fprintf(&b, "[命令失败: %v,耗时 %s]\n", err, elapsed)
	default:
		fmt.Fprintf(&b, "[命令成功,耗时 %s]\n", elapsed)
	}

	if stdout != "" {
		b.WriteString("--- stdout ---\n")
		b.WriteString(truncateMiddle(stdout, maxOutputLen))
	}
	if stderr != "" {
		b.WriteString("\n--- stderr ---\n")
		b.WriteString(truncateMiddle(stderr, 4_000))
	}
	if stdout == "" && stderr == "" {
		b.WriteString("(无输出)")
	}
	return b.String()
}

// truncateMiddle 保留首尾、截掉中间——命令输出的开头(表头/前几行)
// 和结尾(汇总/错误)通常比中段更有信息量。
func truncateMiddle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	half := max / 2
	return s[:half] + fmt.Sprintf("\n...[中间截断 %d 字节]...\n", len(s)-max) + s[len(s)-half:]
}