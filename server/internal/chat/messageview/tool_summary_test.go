package messageview

import (
	"strings"
	"testing"
)

// TestLongAuthorizedToolPathIsNotDropped 验证展示字符预算不能删除合法长路径事实。
func TestLongAuthorizedToolPathIsNotDropped(t *testing.T) {
	relative := strings.Repeat("nested/", 25) + "main.go"
	summary := buildToolSafeSummary(map[string]interface{}{"path": "/workspace/" + relative}, workspacePathScope{workspaceDirectory: "/workspace"})
	if summary == nil || summary.Path != relative {
		t.Fatalf("long path lost: %+v", summary)
	}
	windows, ok := safeWindowsProjectRelativePath(`C:\workspace\`+strings.ReplaceAll(relative, "/", `\`), `C:\workspace`)
	if !ok || windows != relative {
		t.Fatalf("long Windows path lost: %q, %v", windows, ok)
	}
}
