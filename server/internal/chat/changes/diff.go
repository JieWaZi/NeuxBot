package changes

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/go-git/go-git/v5/plumbing"
)

// Line 是可直接渲染的服务端差异行。
type Line struct {
	// Kind 为 context、add 或 delete。
	Kind string `json:"kind"`
	// Text 保留原始行文本，去掉行尾换行。
	Text string `json:"text"`
	// NoNewline 表示该侧原文件末行没有换行。
	NoNewline bool `json:"noNewline,omitempty"`
}

// Hunk 保存连续变化及三行上下文，不让客户端再比较全文。
type Hunk struct {
	// OldStart 是开始侧的一起始行号。
	OldStart int `json:"oldStart"`
	// OldLines 是该 hunk 已返回的开始侧行数。
	OldLines int `json:"oldLines"`
	// NewStart 是结束侧的一起始行号。
	NewStart int `json:"newStart"`
	// NewLines 是该 hunk 已返回的结束侧行数。
	NewLines int `json:"newLines"`
	// Lines 是按原顺序保存的展示行。
	Lines []Line `json:"lines"`
}

// Diff 是与摘要同源、可按需读取的不可变有界差异。
type Diff struct {
	// RecordID 关联唯一成果记录。
	RecordID string `json:"recordId"`
	// RootID 关联原授权根。
	RootID string `json:"rootId"`
	// Path 是结束侧路径，删除时使用开始侧路径。
	Path string `json:"path"`
	// State 为 text、binary、oversized 或 unreadable。
	State string `json:"state"`
	// BeforeExists 区分原文件不存在与空文件。
	BeforeExists bool `json:"beforeExists"`
	// AfterExists 区分删除与空文件。
	AfterExists bool `json:"afterExists"`
	// BeforeMode 是开始侧 Git mode；零表示不存在。
	BeforeMode uint32 `json:"beforeMode,omitempty"`
	// AfterMode 是结束侧 Git mode；零表示不存在。
	AfterMode uint32 `json:"afterMode,omitempty"`
	// Added 是本计划完整新增行数，未知时为空。
	Added *int `json:"added,omitempty"`
	// Deleted 是本计划完整删除行数，未知时为空。
	Deleted *int `json:"deleted,omitempty"`
	// Coarse 表示预算降级为整段替换。
	Coarse bool `json:"coarse,omitempty"`
	// Truncated 表示返回 hunk 受到行数或字节预算限制。
	Truncated bool `json:"truncated,omitempty"`
	// Hunks 是有界差异内容。
	Hunks []Hunk `json:"hunks"`
}

type comparison struct {
	// file 是对调用方开放的路径和统计。
	file File
	// before 是开始侧对象；nil 明确表示不存在。
	before *entry
	// after 是结束侧对象；nil 明确表示不存在。
	after *entry
}

func sameEntry(a, b entry) bool {
	return a.Blob != "" && a.Blob == b.Blob && a.Mode == b.Mode && a.Content != "unreadable" && b.Content != "unreadable"
}

func coveredBy(path string, coverage []Coverage) bool {
	for _, item := range coverage {
		if item.Path == "." || path == item.Path || strings.HasPrefix(path, item.Path+"/") {
			return true
		}
	}
	return false
}

func (c *Capture) compare(ctx context.Context) error {
	summary := &c.record.Summary
	summary.State, summary.Complete = "ready", true
	values := []comparison{}
	for i, root := range summary.Roots {
		before, after := c.record.Before[i], c.record.After[i]
		summary.Roots[i].Boundaries = append(append([]Coverage(nil), before.Boundaries...), after.Boundaries...)
		if len(before.Missing) > 0 || len(after.Missing) > 0 {
			summary.Complete = false
			summary.Roots[i].Boundaries = append(summary.Roots[i].Boundaries, before.Missing...)
			summary.Roots[i].Boundaries = append(summary.Roots[i].Boundaries, after.Missing...)
		}
		paths := map[string]bool{}
		for path := range before.Entries {
			paths[path] = true
		}
		for path := range after.Entries {
			paths[path] = true
		}
		rootChanges := map[string]comparison{}
		for path := range paths {
			a, aOK := before.Entries[path]
			b, bOK := after.Entries[path]
			if aOK && bOK && sameEntry(a, b) {
				continue
			}
			// 内容采集失败只说明覆盖不完整；完整指纹仍可证明未保存 Blob 的大文件变更。
			fingerprintChanged := a.Fingerprint != "" && b.Fingerprint != "" && a.Fingerprint != b.Fingerprint
			if aOK && bOK && (a.Blob == "" || b.Blob == "") && !fingerprintChanged && (a.Mode == 0 || b.Mode == 0 || a.Mode == b.Mode) {
				continue
			}
			// 未枚举到的路径可能只是超出采集范围，不能据此断言新增或删除。
			if !aOK && (coveredBy(path, before.Missing) || coveredBy(path, before.Boundaries)) || !bOK && (coveredBy(path, after.Missing) || coveredBy(path, after.Boundaries)) {
				summary.Complete = false
				continue
			}
			value := comparison{file: File{RootID: root.ID, Path: path, AbsolutePath: root.absolutePath(path), Status: "modified", Content: "text"}}
			if aOK {
				v := a
				value.before = &v
			}
			if bOK {
				v := b
				value.after = &v
			}
			if !aOK {
				value.file.Status = "added"
			}
			if !bOK {
				value.file.Status = "deleted"
			}
			for _, side := range []*entry{value.before, value.after} {
				if side == nil {
					continue
				}
				if side.Content == "unreadable" || side.Blob == "" {
					value.file.Content = "unreadable"
					break
				}
				if side.Content == "oversized" {
					value.file.Content = "oversized"
				} else if side.Content == "binary" && value.file.Content == "text" {
					value.file.Content = "binary"
				}
			}
			rootChanges[path] = value
		}
		if before.Tree != "" && after.Tree != "" && before.Tree != after.Tree {
			data, err := runGit(ctx, c.options.GitExecutable, base(c.owned), repositoryPath(c.owned), "", c.options.Limits, "diff-tree", "-r", "--no-commit-id", "--raw", "-z", "-M", "-l500", "--no-ext-diff", "--no-textconv", before.Tree, after.Tree)
			if err != nil {
				return err
			}
			for old, newPath := range renamePaths(data) {
				a, aOK := rootChanges[old]
				b, bOK := rootChanges[newPath]
				if !aOK || !bOK || a.file.Status != "deleted" || b.file.Status != "added" {
					continue
				}
				b.file.Status, b.file.OldPath, b.before = "renamed", old, a.before
				if a.file.Content != "text" {
					b.file.Content = a.file.Content
				}
				rootChanges[newPath] = b
				delete(rootChanges, old)
			}
		}
		for _, value := range rootChanges {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].file.RootID != values[j].file.RootID {
			return values[i].file.RootID < values[j].file.RootID
		}
		return values[i].file.Path < values[j].file.Path
	})
	summary.TotalFiles = len(values)
	summary.Truncated = len(values) > c.options.Limits.MaxFiles
	if summary.Truncated {
		values = values[:c.options.Limits.MaxFiles]
	}
	if !summary.Complete {
		summary.State, summary.Reason = "partial", "coverage_incomplete"
	}
	directory := filepath.Join(filepath.Dir(recordPath(c.owned, summary.ID)), "diffs")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return storageError("create_diffs", err)
	}
	for _, value := range values {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		diff := Diff{RecordID: summary.ID, RootID: value.file.RootID, Path: value.file.Path, State: value.file.Content, BeforeExists: value.before != nil, AfterExists: value.after != nil, Hunks: []Hunk{}}
		if value.before != nil {
			diff.BeforeMode = value.before.Mode
		}
		if value.after != nil {
			diff.AfterMode = value.after.Mode
		}
		if diff.State == "text" {
			old, err := c.blobText(value.before)
			if err != nil {
				return err
			}
			current, err := c.blobText(value.after)
			if err != nil {
				return err
			}
			plan := makeEdits(ctx, old, current, c.options.Limits.DiffTimeout)
			added, deleted := 0, 0
			for _, edit := range plan.edits {
				if edit.kind == "add" {
					added += len(edit.lines)
				}
				if edit.kind == "delete" {
					deleted += len(edit.lines)
				}
			}
			diff.Added, diff.Deleted, diff.Coarse = &added, &deleted, plan.coarse
			diff.Hunks, diff.Truncated = makeHunks(plan.edits, c.options.Limits)
			value.file.Added, value.file.Deleted, value.file.Coarse = diff.Added, diff.Deleted, diff.Coarse
		}
		if err := writeJSON(filepath.Join(directory, identity(value.file.RootID+"/"+value.file.Path)+".json"), &diff); err != nil {
			return storageError("save_diff", err)
		}
		summary.Files = append(summary.Files, value.file)
	}
	return nil
}

func renamePaths(data []byte) map[string]string {
	result := map[string]string{}
	items := bytes.Split(data, []byte{0})
	for i := 0; i < len(items); {
		header := strings.Fields(string(items[i]))
		i++
		if len(header) != 5 || i >= len(items) {
			break
		}
		path := string(items[i])
		i++
		if strings.HasPrefix(header[4], "R") && i < len(items) {
			result[path] = string(items[i])
			i++
		}
	}
	return result
}

func (c *Capture) blobText(value *entry) (string, error) {
	if value == nil {
		return "", nil
	}
	if value.Blob == "" || value.Content != "text" || value.Size > c.options.Limits.MaxTextBytes {
		return "", status_error.WorkspaceResourceTooLarge
	}
	object, err := objectStore(c.owned).EncodedObject(plumbing.BlobObject, plumbing.NewHash(value.Blob))
	if err != nil {
		return "", storageError("read_blob", err)
	}
	if object.Size() > c.options.Limits.MaxTextBytes {
		return "", status_error.WorkspaceResourceTooLarge
	}
	r, err := object.Reader()
	if err != nil {
		return "", storageError("read_blob", err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, c.options.Limits.MaxTextBytes+1))
	if err != nil {
		return "", storageError("read_blob", err)
	}
	if int64(len(data)) > c.options.Limits.MaxTextBytes {
		return "", status_error.WorkspaceResourceTooLarge
	}
	return string(data), nil
}

// ReadDiff 只读取本记录已保存的差异，不根据当前目录内容重新比较。
func ReadDiff(ctx context.Context, ownedDir, recordID, rootID, path string) (*Diff, error) {
	if ctx.Err() != nil {
		return nil, status_error.OperationCanceled
	}
	r, err := loadRecord(ownedDir, recordID)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, file := range r.Summary.Files {
		if file.RootID == rootID && file.Path == path {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, status_error.ChatMessageFileChangeNotFound
	}
	root, err := os.OpenRoot(ownedDir)
	if err != nil {
		return nil, storageError("open_diff_root", err)
	}
	defer root.Close()
	f, err := root.Open(filepath.Join("changes", "records", recordID, "diffs", identity(rootID+"/"+path)+".json"))
	if err != nil {
		return nil, storageError("read_diff", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(r.Limits.MaxOutputBytes)+1))
	if err != nil {
		return nil, storageError("read_diff", err)
	}
	if len(data) > r.Limits.MaxOutputBytes {
		return nil, status_error.WorkspaceResourceTooLarge
	}
	var value Diff
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, storageError("decode_diff", err)
	}
	if value.RecordID != recordID || value.RootID != rootID || value.Path != path {
		return nil, status_error.WorkspaceScopeInvalid
	}
	return &value, nil
}

type edit struct {
	// kind 为 context、add 或 delete。
	kind string
	// lines 保存含原行尾的文本切片。
	lines []string
}

type editPlan struct {
	// edits 是唯一的统计和 hunk 来源。
	edits []edit
	// coarse 表示整段替换降级。
	coarse bool
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func makeEdits(ctx context.Context, before, after string, budget time.Duration) editPlan {
	deadline := time.Now().Add(budget)
	a, b := splitLines(before), splitLines(after)
	prefix, suffix := 0, 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	x, y := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]
	plan := editPlan{}
	push := func(kind string, lines []string) {
		if len(lines) == 0 {
			return
		}
		if len(plan.edits) > 0 && plan.edits[len(plan.edits)-1].kind == kind {
			plan.edits[len(plan.edits)-1].lines = append(plan.edits[len(plan.edits)-1].lines, lines...)
		} else {
			plan.edits = append(plan.edits, edit{kind: kind, lines: append([]string(nil), lines...)})
		}
	}
	push("context", a[:prefix])
	// 有界 LCS 保留常见小修改的精确行差异；高复杂度输入显式整段替换。
	if len(x) == 0 {
		push("add", y)
	} else if len(y) == 0 {
		push("delete", x)
	} else if int64(len(x)+1)*int64(len(y)+1) > 2_000_000 || time.Now().After(deadline) || ctx.Err() != nil {
		plan.coarse = true
		push("delete", x)
		push("add", y)
	} else {
		width := len(y) + 1
		table := make([]uint32, (len(x)+1)*width)
		expired := false
		for i := len(x) - 1; i >= 0; i-- {
			if time.Now().After(deadline) || ctx.Err() != nil {
				expired = true
				break
			}
			for j := len(y) - 1; j >= 0; j-- {
				if x[i] == y[j] {
					table[i*width+j] = table[(i+1)*width+j+1] + 1
				} else {
					table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
				}
			}
		}
		if expired {
			plan.coarse = true
			push("delete", x)
			push("add", y)
		} else {
			i, j := 0, 0
			for i < len(x) && j < len(y) {
				if x[i] == y[j] {
					push("context", x[i:i+1])
					i++
					j++
				} else if table[(i+1)*width+j] >= table[i*width+j+1] {
					push("delete", x[i:i+1])
					i++
				} else {
					push("add", y[j:j+1])
					j++
				}
			}
			push("delete", x[i:])
			push("add", y[j:])
		}
	}
	push("context", a[len(a)-suffix:])
	return plan
}

func makeHunks(edits []edit, limits Limits) ([]Hunk, bool) {
	hunks := []Hunk{}
	oldLine, newLine, used, usedBytes := 1, 1, 0, 1024
	truncated := false
	for index := 0; index < len(edits); {
		if edits[index].kind == "context" {
			oldLine += len(edits[index].lines)
			newLine += len(edits[index].lines)
			index++
			continue
		}
		start := index
		end := index
		for end+1 < len(edits) {
			if edits[end+1].kind == "context" && len(edits[end+1].lines) > 6 {
				break
			}
			end++
			if end == len(edits)-1 && edits[end].kind == "context" {
				break
			}
		}
		before := []string{}
		if start > 0 && edits[start-1].kind == "context" {
			lines := edits[start-1].lines
			before = lines[max(0, len(lines)-3):]
		}
		hunk := Hunk{OldStart: oldLine - len(before), NewStart: newLine - len(before), Lines: []Line{}}
		appendLines := func(kind string, lines []string) {
			for _, text := range lines {
				line := Line{Kind: kind, Text: strings.TrimSuffix(text, "\n"), NoNewline: !strings.HasSuffix(text, "\n")}
				encoded, _ := json.Marshal(line)
				if used >= limits.MaxDiffLines || usedBytes+len(encoded)+256 > limits.MaxOutputBytes {
					truncated = true
					return
				}
				used++
				usedBytes += len(encoded) + 128
				hunk.Lines = append(hunk.Lines, line)
				if kind != "add" {
					hunk.OldLines++
				}
				if kind != "delete" {
					hunk.NewLines++
				}
			}
		}
		appendLines("context", before)
		for i := start; i <= end; i++ {
			lines := edits[i].lines
			if edits[i].kind == "context" && i == end {
				lines = lines[:min(3, len(lines))]
			}
			appendLines(edits[i].kind, lines)
			if edits[i].kind != "add" {
				oldLine += len(edits[i].lines)
			}
			if edits[i].kind != "delete" {
				newLine += len(edits[i].lines)
			}
		}
		if end+1 < len(edits) && edits[end+1].kind == "context" {
			appendLines("context", edits[end+1].lines[:min(3, len(edits[end+1].lines))])
		}
		if len(hunk.Lines) > 0 {
			hunks = append(hunks, hunk)
		}
		index = end + 1
		if truncated {
			return hunks, true
		}
	}
	return hunks, truncated
}
