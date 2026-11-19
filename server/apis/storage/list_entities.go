package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取伙伴与会话占用
type ListStorageEntities struct {
	ginx.MethodGet
	// 列表类型
	Kind string `in:"query" name:"kind" validate:"required,oneof=wakers chats"`
	// 伙伴标识
	WakerID string `in:"query" name:"wakerId" validate:"max=32"`
	// 分页游标
	Cursor string `in:"query" name:"cursor" validate:"max=160"`
	// 分页大小
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// Path 返回实体列表地址。
func (*ListStorageEntities) Path() string { return "/entities" }

// Validate 校验伙伴筛选只用于会话排行。
func (d *ListStorageEntities) Validate(*gin.Context) error {
	if d.WakerID != "" && d.Kind != "chats" {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// EntityUsage 是实体行大小。
type EntityUsage struct {
	// ID 资源标识
	ID string `json:"id"`
	// Title 显示名称
	Title string `json:"title"`
	// WakerID 伙伴标识
	WakerID string `json:"wakerId,omitempty"`
	// Avatar 伙伴头像
	Avatar string `json:"avatar,omitempty"`
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
	scope := d.Kind
	if d.WakerID != "" {
		scope += "/" + d.WakerID
	}
	offset, err := entityOffset(d.Cursor, scope, snapshot.Generation)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	rows := snapshot.RankedEntities(d.Kind, d.WakerID)
	limit := d.Limit
	if limit == 0 {
		limit = 20
	}
	if offset > len(rows) {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	end := min(offset+limit, len(rows))
	page := EntityPage{Items: make([]EntityUsage, 0, end-offset), Total: len(rows)}
	for _, row := range rows[offset:end] {
		page.Items = append(page.Items, EntityUsage{
			ID: row.ID, Title: row.Title, WakerID: row.WakerID, Bytes: row.Bytes, WorkspaceBytes: row.WorkspaceBytes,
			InternalBytes: row.InternalBytes, ChatBytes: row.ChatBytes, FileCount: row.FileCount, ChatCount: row.ChatCount,
		})
	}
	if end < len(rows) {
		page.NextCursor = fmt.Sprintf("%d:%s:%d", snapshot.Generation, scope, end)
	}
	ids := make([]string, 0, len(page.Items))
	for _, row := range page.Items {
		id := row.WakerID
		if id == "" {
			id = row.ID
		}
		ids = append(ids, id)
	}
	profiles, err := wakerProfiles(request, ids)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for i := range page.Items {
		row := &page.Items[i]
		if row.WakerID == "" {
			row.Title = profiles[row.ID].Name
			row.Avatar = profiles[row.ID].Avatar
		} else {
			row.WakerName = profiles[row.WakerID].Name
		}
	}
	return page, nil
}

func entityOffset(cursor, scope string, generation uint64) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	versionText, scopeAndOffset, found := strings.Cut(cursor, ":")
	separator := strings.LastIndexByte(scopeAndOffset, ':')
	if !found || separator < 0 || scopeAndOffset[:separator] != scope {
		return 0, status_error.BadRequest
	}
	version, err := strconv.ParseUint(versionText, 10, 64)
	if err != nil {
		return 0, status_error.BadRequest
	}
	if version != generation {
		return 0, status_error.StorageSnapshotExpired
	}
	offset, err := strconv.Atoi(scopeAndOffset[separator+1:])
	if err != nil || offset < 0 {
		return 0, status_error.BadRequest
	}
	return offset, nil
}

func wakerNames(ctx context.Context, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))
	rows, err := wakerProfiles(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}

func wakerProfiles(ctx context.Context, ids []string) (map[string]models.Waker, error) {
	rows, err := global.WakerControllerManager.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	profiles := make(map[string]models.Waker, len(rows))
	for _, row := range rows {
		profiles[row.ID] = row
	}
	return profiles, nil
}
