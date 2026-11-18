package storage

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetStorageUsage 获取存储占用。
type GetStorageUsage struct {
	ginx.MethodGet
	// Force 重新统计
	Force bool `in:"query" name:"force"`
}

// Path 返回占用概览地址。
func (*GetStorageUsage) Path() string { return "/usage" }

// UsageResponse 是本机存储概览。
type UsageResponse struct {
	// TotalBytes 已知总量
	TotalBytes int64 `json:"totalBytes"`
	// ScannedAt 统计时间
	ScannedAt int64 `json:"scannedAt"`
	// Complete 统计完整
	Complete bool `json:"complete"`
	// DataRoot 程序目录
	DataRoot string `json:"dataRoot"`
	// ManagedRoot 受管目录
	ManagedRoot string `json:"managedRoot"`
	// Categories 分类用量
	Categories []CategoryUsage `json:"categories"`
	// Disks 磁盘容量
	Disks []DiskUsage `json:"disks"`
}

// CategoryUsage 测量分类。
type CategoryUsage struct {
	// Category 存储分类
	Category constants.StorageCategory `json:"category"`
	// Bytes 已知大小
	Bytes int64 `json:"bytes"`
	// Paths 目录位置
	Paths []string `json:"paths"`
}

// DiskUsage 单卷容量。
type DiskUsage struct {
	// Path 显示位置
	Path string `json:"path"`
	// Paths 所在目录
	Paths []string `json:"paths"`
	// TotalBytes 磁盘容量
	TotalBytes uint64 `json:"totalBytes"`
	// FreeBytes 可用容量
	FreeBytes uint64 `json:"freeBytes"`
}

// Output 返回当前本机磁盘占用和分类概览。
// [StorageRootInvalid][50000009702][无法读取存储目录]
// [OperationCanceled][40800000001][操作取消]
func (d *GetStorageUsage) Output(ctx *gin.Context) (interface{}, error) {
	snapshot, err := global.StorageService.Usage(ctx.Request.Context(), d.Force)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	measurement := snapshot.Measurement
	response := UsageResponse{TotalBytes: measurement.Bytes, ScannedAt: snapshot.MeasuredAt.UnixMilli(),
		Complete: measurement.Complete, DataRoot: measurement.Roots.Data, ManagedRoot: measurement.Roots.Managed,
		Categories: make([]CategoryUsage, 0), Disks: make([]DiskUsage, 0, len(measurement.Disks))}
	groups := make(map[constants.StorageCategory]*CategoryUsage)
	seen := make(map[constants.StorageCategory]map[string]bool)
	for _, entry := range measurement.Entries {
		group := groups[entry.Category]
		if group == nil {
			group = &CategoryUsage{Category: entry.Category, Paths: make([]string, 0)}
			groups[entry.Category] = group
			seen[entry.Category] = make(map[string]bool)
		}
		group.Bytes += entry.Bytes
		const displayedPaths = 16
		if len(group.Paths) < displayedPaths {
			relative, err := filepath.Rel(entry.OwnerRoot, entry.Path)
			if err == nil {
				top, _, _ := strings.Cut(relative, string(filepath.Separator))
				path := filepath.Join(entry.OwnerRoot, top)
				if !seen[entry.Category][path] {
					seen[entry.Category][path] = true
					group.Paths = append(group.Paths, path)
				}
			}
		}
	}
	for _, group := range groups {
		sort.Strings(group.Paths)
		response.Categories = append(response.Categories, *group)
	}
	sort.Slice(response.Categories, func(i, j int) bool { return response.Categories[i].Category < response.Categories[j].Category })
	for _, disk := range measurement.Disks {
		if len(disk.Roots) == 0 {
			continue
		}
		response.Disks = append(response.Disks, DiskUsage{Path: disk.Roots[0], Paths: disk.Roots, TotalBytes: disk.TotalBytes, FreeBytes: disk.AvailableBytes})
	}
	return response, nil
}
