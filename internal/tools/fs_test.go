package tools

import (
	"strings"
	"testing"
)

func TestResolveInWorkspace(t *testing.T) {
	ws := "/home/user/ws"

	// 表驱动测试(table-driven):Go 社区的标准形态。
	// 用例即文档——这张表就是 resolveInWorkspace 的行为规格书。
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"正常相对路径", "notes/todo.md", "/home/user/ws/notes/todo.md", false},
		{"当前目录", ".", "/home/user/ws", false},
		{"直接父目录逃逸", "../secret", "", true},
		{"深层逃逸", "../../../etc/passwd", "", true},
		{"迂回逃逸", "a/../../b", "", true},
		{"兄弟目录前缀攻击", "../ws-evil/x", "", true},
		{"折叠后合法", "a/../b.txt", "/home/user/ws/b.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInWorkspace(ws, tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "escapes") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}