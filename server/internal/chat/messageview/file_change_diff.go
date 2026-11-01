package messageview

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
)

// standardACPFileDiffContent 是标准 ACP 单个文件差异的内部完整内容。
type standardACPFileDiffContent struct {
	// path 是桌面端可以安全读取的对话工作区相对路径。
	path string
	// oldContent 是该次工具修改前的完整文本。
	oldContent string
	// newContent 是该次工具修改后的完整文本。
	newContent string
	// hasOldContent 区分新建文件与显式提供空旧文本的修改。
	hasOldContent bool
}

// standardACPFileDiffContents 读取标准 ACP ToolCallContent diff 的完整文本。
func standardACPFileDiffContents(
	data map[string]interface{},
	scope workspacePathScope,
) []standardACPFileDiffContent {
	items, _ := data["content"].([]interface{})
	diffs := make([]standardACPFileDiffContent, 0, len(items))
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]interface{})
		if item == nil || firstDisplayString(item, "type") != "diff" {
			continue
		}
		logicalPath, ok := scope.safeRelativePath(firstDisplayString(item, "path"))
		if !ok {
			continue
		}
		newContent, newOK := item["newText"].(string)
		if !newOK || len(newContent) > constants.MessageFileChangeDiffMaxBytes {
			continue
		}
		oldValue, hasOldContent := item["oldText"]
		oldContent, oldOK := oldValue.(string)
		if hasOldContent && oldValue != nil && !oldOK {
			continue
		}
		hasOldContent = hasOldContent && oldValue != nil
		if len(oldContent) > constants.MessageFileChangeDiffMaxBytes {
			continue
		}
		diffs = append(diffs, standardACPFileDiffContent{
			path:          logicalPath,
			oldContent:    oldContent,
			newContent:    newContent,
			hasOldContent: hasOldContent,
		})
	}
	return diffs
}
