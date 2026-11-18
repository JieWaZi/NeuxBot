package storage

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	service "github.com/JieWaZi/NeuxBot/server/services/storage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListStorageEntities 获取伙伴与会话占用。
type ListStorageEntities struct {
	ginx.MethodGet
	// Kind 列表类型
	Kind string `in:"query" name:"kind" validate:"required,oneof=wakers chats"`
	// Cursor 分页游标
	Cursor string `in:"query" name:"cursor" validate:"max=160"`
	// Limit 分页大小
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// Path 返回实体列表地址。
func (*ListStorageEntities) Path() string { return "/entities" }

// EntityUsage 是实体行大小。
type EntityUsage struct {
	// ID 资源标识
	ID string `json:"id"`
	// Title 显示名称
	Title string `json:"title"`
	// WakerID 伙伴标识
	WakerID string `json:"wakerId,omitempty"`
	// WakerName 伙伴名称
	WakerName string `json:"wakerName,omitempty"`
	// Bytes 占用大小
	Bytes int64 `json:"bytes"`
	// WorkspaceBytes 空间占用
	WorkspaceBytes int64 `json:"workspaceBytes"`
	// InternalBytes 资源占用
	InternalBytes int64 `json:"internalBytes"`
	// ChatBytes 私有占用
	ChatBytes int64 `json:"chatBytes"`
	// FileCount 文件数量
	FileCount int `json:"fileCount"`
	// ChatCount 会话数量
	ChatCount int `json:"chatCount"`
}

// EntityPage 是占用排序页。
type EntityPage struct {
	// Items 排名列表
	Items []EntityUsage `json:"items"`
	// Total 实体数量
	Total int `json:"total"`
	// NextCursor 下一页标识
	NextCursor string `json:"nextCursor,omitempty"`
}

// Output 返回同一统计代次内的占用排序。
func (d *ListStorageEntities) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	snapshot, err := global.StorageService.Usage(request, false)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	offset, err := entityOffset(d.Cursor, d.Kind, snapshot.Generation)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	wakers, chats, err := rankEntities(snapshot)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	rows := chats
	if d.Kind == "wakers" {
		rows = wakers
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Bytes != rows[j].Bytes {
			return rows[i].Bytes > rows[j].Bytes
		}
		return rows[i].ID < rows[j].ID
	})
	limit := d.Limit
	if limit == 0 {
		limit = 20
	}
	if offset > len(rows) {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	end := min(offset+limit, len(rows))
	page := EntityPage{Items: rows[offset:end], Total: len(rows)}
	if end < len(rows) {
		page.NextCursor = fmt.Sprintf("%d:%s:%d", snapshot.Generation, d.Kind, end)
	}
	ids := make([]string, 0, len(page.Items))
	for _, row := range page.Items {
		id := row.WakerID
		if id == "" {
			id = row.ID
		}
		ids = append(ids, id)
	}
	names, err := wakerNames(request, ids)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for i := range page.Items {
		row := &page.Items[i]
		if row.WakerID == "" {
			row.Title = names[row.ID]
		} else {
			row.WakerName = names[row.WakerID]
		}
	}
	return page, nil
}

func entityOffset(cursor, kind string, generation uint64) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	fields := strings.Split(cursor, ":")
	if len(fields) != 3 || fields[1] != kind {
		return 0, status_error.BadRequest
	}
	version, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, status_error.BadRequest
	}
	if version != generation {
		return 0, status_error.StorageSnapshotExpired
	}
	offset, err := strconv.Atoi(fields[2])
	if err != nil || offset < 0 {
		return 0, status_error.BadRequest
	}
	return offset, nil
}

func rankEntities(snapshot service.Snapshot) ([]EntityUsage, []EntityUsage, error) {
	if snapshot.ChatMetadataError != nil {
		return nil, nil, snapshot.ChatMetadataError
	}
	metadata := snapshot.ChatMetadata
	partners := make(map[string]EntityUsage)
	chats := make([]EntityUsage, 0, len(metadata))
	for _, chat := range metadata {
		if chat.HiddenFromWakerHistory {
			continue
		}
		used := snapshot.Chats[chat.ID]
		chats = append(chats, EntityUsage{ID: chat.ID, Title: chat.Title, WakerID: chat.WakerID, Bytes: used.Bytes, ChatBytes: used.Bytes, FileCount: used.FileCount})
		partner := partners[chat.WakerID]
		partner.ID = chat.WakerID
		partner.Bytes += used.Bytes
		partner.ChatBytes += used.Bytes
		partner.FileCount += used.FileCount
		partner.ChatCount++
		partners[chat.WakerID] = partner
	}
	for _, entry := range snapshot.Measurement.Entries {
		if entry.WakerID == "" || entry.ChatID != "" {
			continue
		}
		partner := partners[entry.WakerID]
		partner.ID = entry.WakerID
		partner.Bytes += entry.Bytes
		if entry.Category == constants.StorageCategoryPrograms {
			partner.InternalBytes += entry.Bytes
		} else {
			partner.WorkspaceBytes += entry.Bytes
			partner.FileCount++
		}
		partners[entry.WakerID] = partner
	}
	wakers := make([]EntityUsage, 0, len(partners))
	for _, partner := range partners {
		wakers = append(wakers, partner)
	}
	return wakers, chats, nil
}

func wakerNames(ctx context.Context, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))
	rows, err := global.WakerControllerManager.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}
