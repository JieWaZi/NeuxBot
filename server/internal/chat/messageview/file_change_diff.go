package messageview

import (
	"encoding/json"

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

// toolFileDiffSnapshot 保存一个工具调用最后一次交付的标准 ACP 文件差异。
type toolFileDiffSnapshot struct {
	// toolID 是工具调用和最终状态配对使用的稳定标识。
	toolID string
	// diffs 是该工具调用最后一次完整替换交付的文件差异。
	diffs []standardACPFileDiffContent
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

// messageFileChangeDiffs 把成功文件修改工具合成为执行开始到结束的按文件差异。
func messageFileChangeDiffs(
	events []MessageRuntimeEvent,
	parts []MessageDisplayPart,
	summaries []ToolFileChangeSummary,
	scope workspacePathScope,
) []MessageFileChangeDiff {
	if len(summaries) == 0 {
		return nil
	}
	allowedTools := make(map[string]struct{})
	for _, part := range parts {
		if part.Type == constants.MessageDisplayPartTool &&
			part.ToolCategory == constants.MessageToolCategoryFileModify &&
			part.ToolStatus != constants.MessageToolStatusFailed {
			allowedTools[part.ToolCallID] = struct{}{}
		}
	}
	allowedPaths := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		allowedPaths[summary.Path] = struct{}{}
	}

	toolIndexes := make(map[string]int)
	toolSnapshots := make([]toolFileDiffSnapshot, 0)
	for _, event := range events {
		if event.Type != "tool_use" && event.Type != "tool_result" {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			continue
		}
		toolID := displayToolID(data, event)
		if _, ok := allowedTools[toolID]; !ok {
			continue
		}
		diffs := standardACPFileDiffContents(data, scope)
		if len(diffs) == 0 {
			continue
		}
		if index, exists := toolIndexes[toolID]; exists {
			toolSnapshots[index].diffs = diffs
			continue
		}
		toolIndexes[toolID] = len(toolSnapshots)
		toolSnapshots = append(toolSnapshots, toolFileDiffSnapshot{
			toolID: toolID,
			diffs:  diffs,
		})
	}

	changeIndexes := make(map[string]int)
	changes := make([]MessageFileChangeDiff, 0, len(summaries))
	for _, tool := range toolSnapshots {
		for _, diff := range tool.diffs {
			if _, ok := allowedPaths[diff.path]; !ok {
				continue
			}
			if index, exists := changeIndexes[diff.path]; exists {
				changes[index].NewContent = diff.newContent
				continue
			}
			changeIndexes[diff.path] = len(changes)
			changes = append(changes, MessageFileChangeDiff{
				Path:       diff.path,
				OldContent: diff.oldContent,
				NewContent: diff.newContent,
			})
		}
	}
	return changes
}
