package messageview

import (
	"net/url"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// workspacePathScope 保存消息文件路径映射需要的受管工作区和真实项目入口。
type workspacePathScope struct {
	// workspaceDirectory 是消息相对路径使用的对话执行工作区根目录。
	workspaceDirectory string
	// projectSources 是真实项目绝对路径到 projects 命名空间的冻结映射。
	projectSources []eventlog.ProjectSourceSnapshot
}

func buildToolSafeSummary(data map[string]interface{}, scope workspacePathScope) *ToolSafeSummary {
	summary := &ToolSafeSummary{}
	files := standardACPFileChanges(data, scope)
	if len(files) > 0 {
		summary.Files = files
		summary.Path = files[0].Path
		added, deleted := aggregateFileChangeStats(files)
		summary.AddedLines = &added
		summary.DeletedLines = &deleted
	}
	pathValue := firstNestedDisplayString(data, "file_path", "filePath", "path")
	if pathValue == "" {
		pathValue = firstStructuredToolPath(data)
	}
	if pathValue != "" {
		if safePath, ok := scope.safeRelativePath(pathValue); ok {
			summary.Path = safePath
		}
	}
	rawURL := firstNestedDisplayString(data, "url")
	if rawURL == "" {
		if action := webSearchActionObject(data); action != nil {
			rawURL = firstDisplayString(action, "url")
		}
	}
	if rawURL != "" {
		if domain, ok := safeURLDomain(rawURL); ok {
			summary.Domain = domain
		}
	}
	if summary.AddedLines == nil {
		if value, ok := optionalBoundedInt(data, "added_lines", "addedLines"); ok {
			summary.AddedLines = &value
		}
	}
	if summary.DeletedLines == nil {
		if value, ok := optionalBoundedInt(data, "deleted_lines", "deletedLines", "removed_lines", "removedLines"); ok {
			summary.DeletedLines = &value
		}
	}
	if summary.Path == "" && summary.Domain == "" &&
		summary.AddedLines == nil && summary.DeletedLines == nil && len(summary.Files) == 0 {
		return nil
	}
	return summary
}

// standardACPFileChanges 从标准 ACP ToolCallContent diff 列表构建逐文件摘要。
func standardACPFileChanges(
	data map[string]interface{},
	scope workspacePathScope,
) []ToolFileChangeSummary {
	diffs := standardACPFileDiffContents(data, scope)
	changes := make([]ToolFileChangeSummary, 0, len(diffs))
	indexes := make(map[string]int)
	for _, diff := range diffs {
		added, deleted, ok := standardDiffStats(
			diff.oldContent,
			diff.newContent,
			diff.hasOldContent,
		)
		if !ok {
			continue
		}
		if _, exists := indexes[diff.path]; !exists &&
			len(changes) >= constants.MessageToolSafeSummaryMaxFiles {
			continue
		}
		changes = mergeFileChangeSummary(changes, indexes, ToolFileChangeSummary{
			Path:         diff.path,
			AddedLines:   added,
			DeletedLines: deleted,
		})
	}
	return changes
}

// safeRelativePath 把受管工作区或真实项目绝对路径转换为桌面端可读取的逻辑路径。
func (scope workspacePathScope) safeRelativePath(rawPath string) (string, bool) {
	if relative, ok := safeProjectRelativePath(rawPath, scope.workspaceDirectory); ok {
		return relative, true
	}
	if !filepath.IsAbs(rawPath) && windowsVolume(normalizeWindowsPath(rawPath)) == "" {
		return "", false
	}
	bestPath := ""
	bestSourceLength := -1
	for _, source := range scope.projectSources {
		if !safeProjectEntryName(source.EntryName) {
			continue
		}
		relative, ok := safeProjectRelativePath(rawPath, source.SourcePath)
		if !ok {
			continue
		}
		logicalPath := pathpkg.Join("projects", source.EntryName, filepath.ToSlash(relative))
		logicalPath = truncateSummaryString(logicalPath)
		if logicalPath == "" {
			continue
		}
		sourceLength := len(normalizeWindowsPath(filepath.Clean(source.SourcePath)))
		if sourceLength <= bestSourceLength {
			continue
		}
		bestPath = logicalPath
		bestSourceLength = sourceLength
	}
	return bestPath, bestPath != ""
}

// safeProjectEntryName 校验冻结入口可以安全组成 projects 下的单级路径。
func safeProjectEntryName(value string) bool {
	return value != "" && value != "." && value != ".." &&
		pathpkg.Base(value) == value && !strings.ContainsAny(value, "\\\x00\r\n")
}

// mergeFileChangeSummary 按首次出现顺序合并同一文件的多个 diff hunk。
func mergeFileChangeSummary(
	changes []ToolFileChangeSummary,
	indexes map[string]int,
	change ToolFileChangeSummary,
) []ToolFileChangeSummary {
	if index, exists := indexes[change.Path]; exists {
		added := changes[index].AddedLines + change.AddedLines
		deleted := changes[index].DeletedLines + change.DeletedLines
		if added <= constants.MessageToolSafeSummaryMaxInt &&
			deleted <= constants.MessageToolSafeSummaryMaxInt {
			changes[index].AddedLines = added
			changes[index].DeletedLines = deleted
		}
		return changes
	}
	indexes[change.Path] = len(changes)
	return append(changes, change)
}

// aggregateFileChangeStats 汇总逐文件统计，超出安全上限时截断到上限。
func aggregateFileChangeStats(changes []ToolFileChangeSummary) (int, int) {
	added, deleted := 0, 0
	for _, change := range changes {
		added += change.AddedLines
		deleted += change.DeletedLines
		if added > constants.MessageToolSafeSummaryMaxInt {
			added = constants.MessageToolSafeSummaryMaxInt
		}
		if deleted > constants.MessageToolSafeSummaryMaxInt {
			deleted = constants.MessageToolSafeSummaryMaxInt
		}
	}
	return added, deleted
}

// standardDiffStats 计算标准 ACP oldText/newText 的逐行编辑量。
func standardDiffStats(oldText, newText string, hasOldText bool) (int, int, bool) {
	oldCount, newCount := textLineCount(oldText), textLineCount(newText)
	if oldCount > constants.MessageToolSafeSummaryMaxInt ||
		newCount > constants.MessageToolSafeSummaryMaxInt {
		return 0, 0, false
	}
	if !hasOldText {
		return newCount, 0, true
	}
	if newText == "" {
		return 0, oldCount, true
	}
	return lineEditStats(splitTextLines(oldText), splitTextLines(newText))
}

// lineEditStats 使用 Myers 最短编辑路径计算新增与删除行数。
func lineEditStats(oldLines, newLines []string) (int, int, bool) {
	oldCount, newCount := len(oldLines), len(newLines)
	if oldCount == 0 {
		return newCount, 0, true
	}
	if newCount == 0 {
		return 0, oldCount, true
	}
	maxDistance := oldCount + newCount
	frontier := make([]int, 2*maxDistance+3)
	offset := maxDistance + 1
	operations := 0
	for distance := 0; distance <= maxDistance; distance++ {
		for diagonal := -distance; diagonal <= distance; diagonal += 2 {
			operations++
			if operations > constants.MessageToolSafeSummaryMaxInt*4 {
				return 0, 0, false
			}
			index := offset + diagonal
			var oldIndex int
			if diagonal == -distance ||
				(diagonal != distance && frontier[index-1] < frontier[index+1]) {
				oldIndex = frontier[index+1]
			} else {
				oldIndex = frontier[index-1] + 1
			}
			newIndex := oldIndex - diagonal
			for oldIndex < oldCount && newIndex < newCount &&
				oldLines[oldIndex] == newLines[newIndex] {
				oldIndex++
				newIndex++
			}
			frontier[index] = oldIndex
			if oldIndex >= oldCount && newIndex >= newCount {
				delta := newCount - oldCount
				return (distance + delta) / 2, (distance - delta) / 2, true
			}
		}
	}
	return 0, 0, false
}

// splitTextLines 按逻辑行拆分完整文本，不把文件尾换行计为额外空行。
func splitTextLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.SplitAfter(value, "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

// textLineCount 返回完整文本中的逻辑行数。
func textLineCount(value string) int {
	if value == "" {
		return 0
	}
	count := strings.Count(value, "\n")
	if !strings.HasSuffix(value, "\n") {
		count++
	}
	return count
}

func firstStructuredToolPath(data map[string]interface{}) string {
	for _, candidate := range nestedToolObjects(data) {
		items, _ := candidate["commandActions"].([]interface{})
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]interface{})
			if pathValue := firstDisplayString(item, "path"); pathValue != "" {
				return pathValue
			}
		}
	}
	return ""
}

func mergeToolSafeSummary(left *ToolSafeSummary, right *ToolSafeSummary) *ToolSafeSummary {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	merged := *left
	merged.Files = append([]ToolFileChangeSummary(nil), left.Files...)
	if len(right.Files) > 0 {
		// ACP ToolCallUpdate 的 content 是当前工具内容的完整替换值。
		// 结果阶段的可靠 diff 必须替换开始阶段的乐观摘要，避免重复计数。
		merged.Files = append([]ToolFileChangeSummary(nil), right.Files...)
		if right.Path != "" {
			merged.Path = right.Path
		}
		if right.AddedLines != nil {
			merged.AddedLines = right.AddedLines
		}
		if right.DeletedLines != nil {
			merged.DeletedLines = right.DeletedLines
		}
	}
	if merged.Path == "" {
		merged.Path = right.Path
	}
	if merged.Domain == "" {
		merged.Domain = right.Domain
	}
	if merged.AddedLines == nil {
		merged.AddedLines = right.AddedLines
	}
	if merged.DeletedLines == nil {
		merged.DeletedLines = right.DeletedLines
	}
	return &merged
}

// messageFileChanges 把同一 Assistant 消息内的文件修改工具聚合为执行级摘要。
func messageFileChanges(parts []MessageDisplayPart) []ToolFileChangeSummary {
	changes := make([]ToolFileChangeSummary, 0)
	indexes := make(map[string]int)
	for _, part := range parts {
		if part.Type != constants.MessageDisplayPartTool ||
			part.ToolCategory != constants.MessageToolCategoryFileModify ||
			part.ToolStatus == constants.MessageToolStatusFailed ||
			part.ToolSafeSummary == nil {
			continue
		}
		partChanges := part.ToolSafeSummary.Files
		if len(partChanges) == 0 && part.ToolSafeSummary.Path != "" {
			partChanges = []ToolFileChangeSummary{{
				Path:         part.ToolSafeSummary.Path,
				AddedLines:   optionalSummaryInt(part.ToolSafeSummary.AddedLines),
				DeletedLines: optionalSummaryInt(part.ToolSafeSummary.DeletedLines),
			}}
		}
		for _, change := range partChanges {
			if _, exists := indexes[change.Path]; !exists &&
				len(changes) >= constants.MessageToolSafeSummaryMaxFiles {
				continue
			}
			changes = mergeFileChangeSummary(changes, indexes, change)
		}
	}
	return changes
}

// optionalSummaryInt 把工具摘要中的可选统计转换为执行级摘要使用的零值。
func optionalSummaryInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func firstNestedDisplayString(data map[string]interface{}, keys ...string) string {
	for _, candidate := range nestedToolObjects(data) {
		if candidate == nil {
			continue
		}
		if value := firstDisplayString(candidate, keys...); value != "" {
			return value
		}
	}
	return ""
}

func nestedObject(data map[string]interface{}, key string) map[string]interface{} {
	value, ok := data[key].(map[string]interface{})
	if !ok {
		return nil
	}
	return value
}

func optionalBoundedInt(data map[string]interface{}, keys ...string) (int, bool) {
	value, ok := optionalInt(data, keys...)
	if !ok || value > constants.MessageToolSafeSummaryMaxInt || value < -constants.MessageToolSafeSummaryMaxInt {
		return 0, false
	}
	return value, true
}

func optionalInt(data map[string]interface{}, keys ...string) (int, bool) {
	for _, candidate := range nestedToolObjects(data) {
		if candidate == nil {
			continue
		}
		for _, key := range keys {
			switch value := candidate[key].(type) {
			case int:
				return value, true
			case int64:
				return int(value), true
			case float64:
				if value == float64(int(value)) {
					return int(value), true
				}
			}
		}
	}
	return 0, false
}

func safeURLDomain(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	domain := truncateSummaryString(strings.ToLower(parsed.Hostname()))
	if domain == "" {
		return "", false
	}
	return domain, true
}

func safeProjectRelativePath(rawPath string, workingDirectory string) (string, bool) {
	if rawPath == "" || workingDirectory == "" {
		return "", false
	}
	if looksWindowsPath(rawPath) || looksWindowsPath(workingDirectory) {
		return safeWindowsProjectRelativePath(rawPath, workingDirectory)
	}
	if !filepath.IsAbs(workingDirectory) {
		return "", false
	}
	workingDirectory = filepath.Clean(workingDirectory)
	target := rawPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(workingDirectory, target)
	}
	target = filepath.Clean(target)
	relative, err := filepath.Rel(workingDirectory, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", false
	}
	return truncateSummaryString(filepath.ToSlash(relative)), true
}

func safeWindowsProjectRelativePath(rawPath string, workingDirectory string) (string, bool) {
	workingDirectory = normalizeWindowsPath(workingDirectory)
	rawPath = normalizeWindowsPath(rawPath)
	workingVolume := windowsVolume(workingDirectory)
	if workingVolume == "" {
		return "", false
	}
	target := rawPath
	if volume := windowsVolume(target); volume != "" {
		if !strings.EqualFold(volume, workingVolume) {
			return "", false
		}
	} else {
		target = strings.TrimSuffix(workingDirectory, "/") + "/" + target
	}
	workingClean := pathpkg.Clean(strings.TrimPrefix(workingDirectory, workingVolume))
	targetClean := pathpkg.Clean(strings.TrimPrefix(target, workingVolume))
	basePrefix := strings.TrimSuffix(workingClean, "/") + "/"
	if targetClean == workingClean || !strings.HasPrefix(targetClean, basePrefix) {
		return "", false
	}
	relative := strings.TrimPrefix(targetClean, basePrefix)
	return truncateSummaryString(relative), true
}

func looksWindowsPath(value string) bool {
	return windowsVolume(value) != "" || strings.Contains(value, "\\")
}

func normalizeWindowsPath(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func windowsVolume(value string) string {
	if len(value) < 2 || value[1] != ':' {
		return ""
	}
	first := value[0]
	if (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') {
		return strings.ToUpper(value[:2])
	}
	return ""
}

func truncateSummaryString(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > constants.MessageToolSafeSummaryMaxRunes {
		return ""
	}
	return value
}
