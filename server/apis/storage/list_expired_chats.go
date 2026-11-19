package storage

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListExpiredChats 读取过期会话。
type ListExpiredChats struct {
	ginx.MethodGet
	// RetentionDays 保留天数
	RetentionDays int `in:"query" name:"retentionDays" validate:"min=0,max=3650"`
	// Cursor 分页游标
	Cursor string `in:"query" name:"cursor" validate:"max=160"`
	// Limit 分页大小
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// Path 返回过期候选地址。
func (*ListExpiredChats) Path() string { return "/expired-chats" }

// ExpiredChatsPage 是过期会话候选和当前已知私有占用。
type ExpiredChatsPage struct {
	// Items 会话列表
	Items []ExpiredChat `json:"items"`
	// Total 候选总数
	Total int64 `json:"total"`
	// KnownBytes 已知大小
	KnownBytes int64 `json:"knownBytes"`
	// Complete 大小完整
	Complete bool `json:"complete"`
	// NextCursor 下一页标识
	NextCursor string `json:"nextCursor,omitempty"`
}

// ExpiredChat 表示一个过期会话。
type ExpiredChat struct {
	// ChatID 会话标识
	ChatID string `json:"chatId"`
	// Title 会话名称
	Title string `json:"title"`
	// WakerID 伙伴标识
	WakerID string `json:"wakerId"`
	// WakerName 伙伴名称
	WakerName string `json:"wakerName"`
	// LastActivityAt 最后活动
	LastActivityAt int64 `json:"lastActivityAt"`
	// Bytes 私有大小
	Bytes int64 `json:"bytes"`
	// SizeKnown 大小已知
	SizeKnown bool `json:"sizeKnown"`
	// Files 私有目录
	Files []PrivateFolder `json:"files"`
}

// PrivateFolder 是一种测得的会话私有目录。
type PrivateFolder struct {
	// Category 目录类别
	Category string `json:"category"`
	// Path 存储位置
	Path string `json:"path"`
	// Bytes 已知大小
	Bytes int64 `json:"bytes"`
}

// Output 读取未保护且归属普通伙伴的候选，合并已测私有占用。
func (d *ListExpiredChats) Output(ctx *gin.Context) (interface{}, error) {
	req := ctx.Request.Context()
	days := d.RetentionDays
	if days == 0 {
		days = global.StorageService.Policy().RetentionDays
	}
	cutoff, activity, id, err := readExpirationCursor(d.Cursor, days)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	limit := d.Limit
	if limit == 0 {
		limit = 20
	}
	rows, total, cutoff, err := global.StorageService.ListExpired(req, days, cutoff, activity, id, limit+1)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	page := ExpiredChatsPage{Items: make([]ExpiredChat, 0, min(len(rows), limit)), Total: total, Complete: total == 0}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		page.NextCursor = fmt.Sprintf("%d:%d:%d:%s", days, cutoff, last.LastActivityAt, last.ChatID)
	}
	if len(rows) == 0 {
		return page, nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.WakerID)
	}
	names, err := wakerNames(req, ids)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	snapshot, measured := global.StorageService.CachedUsage()
	page.Complete = measured && snapshot.Measurement.Complete && int64(len(rows)) == total && activity == 0
	for _, row := range rows {
		used, known := snapshot.Chats[row.ChatID]
		files := make([]PrivateFolder, 0, len(used.Folders))
		for path, bytes := range used.Folders {
			files = append(files, PrivateFolder{Category: filepath.Base(path), Path: path, Bytes: bytes})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Category < files[j].Category })
		page.Items = append(page.Items, ExpiredChat{ChatID: row.ChatID, Title: row.Title, WakerID: row.WakerID, WakerName: names[row.WakerID], LastActivityAt: row.LastActivityAt, Bytes: used.Bytes, SizeKnown: known && measured && snapshot.Measurement.Complete, Files: files})
		page.KnownBytes += used.Bytes
		page.Complete = page.Complete && known
	}
	return page, nil
}

func readExpirationCursor(cursor string, days int) (int64, int64, string, error) {
	if cursor == "" {
		return 0, 0, "", nil
	}
	fields := strings.Split(cursor, ":")
	if len(fields) != 4 || fields[0] != strconv.Itoa(days) || fields[3] == "" || len(fields[3]) > constants.StorageCleanupBatchSize {
		return 0, 0, "", status_error.BadRequest
	}
	cutoff, e1 := strconv.ParseInt(fields[1], 10, 64)
	activity, e2 := strconv.ParseInt(fields[2], 10, 64)
	if e1 != nil || e2 != nil || cutoff <= 0 || activity <= 0 || activity >= cutoff {
		return 0, 0, "", status_error.BadRequest
	}
	return cutoff, activity, fields[3], nil
}
