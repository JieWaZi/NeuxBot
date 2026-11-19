package storage

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListStorageFiles 列出占用明细文件。
type ListStorageFiles struct {
	ginx.MethodGet
	// 对象类型
	Kind string `in:"query" name:"kind" validate:"required,oneof=wakers chats"`
	// 对象标识
	ID string `in:"query" name:"id" validate:"required,max=32"`
	// 搜索词
	Search string `in:"query" name:"search" validate:"max=200"`
	// 排序方式
	Sort string `in:"query" name:"sort" validate:"omitempty,oneof=latest largest"`
	// 分页游标
	Cursor string `in:"query" name:"cursor" validate:"max=160"`
}

// StorageFile 文件明细行。
type StorageFile struct {
	// 文件标识
	ID string `json:"id"`
	// 文件路径
	RelativePath string `json:"relativePath"`
	// 文件来源
	Source string `json:"source"`
	// 会话标题
	ChatTitle string `json:"chatTitle,omitempty"`
	// MessageID 上传消息标识
	MessageID string `json:"messageId,omitempty"`
	// 文件大小
	Bytes int64 `json:"bytes"`
	// 修改时间
	ModifiedAt int64 `json:"modifiedAt"`
	// 文件版本
	Version string `json:"version"`
}

// StorageFilePage 存储文件列表。
type StorageFilePage struct {
	// 对象名称
	Title string `json:"title"`
	// 文件列表
	Items []StorageFile `json:"items"`
	// 文件总数
	Total int `json:"total"`
	// 文件总量
	TotalBytes int64 `json:"totalBytes"`
	// 扫描完整
	Complete bool `json:"complete"`
	// 后续游标
	NextCursor string `json:"nextCursor,omitempty"`
}

// Path 返回存储文件清单地址。
func (*ListStorageFiles) Path() string { return "/files" }

// Output 从同一统计代次读取可管理的受管文件。
func (d *ListStorageFiles) Output(ctx *gin.Context) (interface{}, error) {
	var title string
	if d.Kind == "wakers" {
		waker, err := global.WakerControllerManager.Get(ctx, d.ID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		title = waker.Name
	} else {
		chat, err := global.ChatControllerManager.Get(ctx, d.ID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if chat.HiddenFromWakerHistory {
			return nil, ginx.WithStack(status_error.ChatNotFound)
		}
		title = chat.Title
	}
	snapshot, err := global.StorageService.Usage(ctx.Request.Context(), false)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if snapshot.ChatMetadataError != nil {
		return nil, ginx.WithStack(snapshot.ChatMetadataError)
	}
	search := strings.ToLower(strings.TrimSpace(d.Search))
	largest := d.Sort == "largest"
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%t/%s", d.Kind, d.ID, largest, search))))
	offset, err := entityOffset(d.Cursor, scope, snapshot.Generation)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	const pageSize = 50
	files, total, bytes, complete, err := snapshot.UserFilesPage(ctx.Request.Context(), d.Kind, d.ID, search, largest, offset, pageSize)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if offset > total {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	response := StorageFilePage{Title: title, Items: make([]StorageFile, 0, len(files)), Total: total, TotalBytes: bytes, Complete: complete}
	for _, file := range files {
		response.Items = append(response.Items, StorageFile{
			ID: file.ID, RelativePath: file.RelativePath, Source: file.Source, ChatTitle: file.ChatTitle, MessageID: file.MessageID,
			Bytes: file.Entry.Bytes, ModifiedAt: file.Entry.ModifiedAt.UnixMilli(), Version: file.Entry.UserFileVersion(),
		})
	}
	if end := offset + len(files); end < total {
		response.NextCursor = fmt.Sprintf("%d:%s:%d", snapshot.Generation, scope, end)
	}
	return response, nil
}
