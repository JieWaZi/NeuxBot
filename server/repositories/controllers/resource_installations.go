package controllers

import (
	"context"

	"gorm.io/gorm"
)

// resourceInstallationCount 是有界分组统计的扫描结果，不复制资源事实。
type resourceInstallationCount struct {
	// ID 是被安装资源的稳定身份。
	ID string
	// Count 是实际安装关系数量，含停用关系。
	Count int64
}

func installedResourceCounts(ctx context.Context, db *gorm.DB, model any, column string, ids []string) (map[string]int64, error) {
	result := map[string]int64{}
	for offset := 0; offset < len(ids); offset += 200 {
		end := offset + 200
		if end > len(ids) {
			end = len(ids)
		}
		var counts []resourceInstallationCount
		err := controllerDB(ctx, db).Model(model).Select(column+" AS id, COUNT(*) AS count").Where(column+" IN ?", ids[offset:end]).Group(column).Find(&counts).Error
		if err != nil {
			return nil, databaseError("count_resource_installations", column, err)
		}
		for _, item := range counts {
			result[item.ID] = item.Count
		}
	}
	return result, nil
}
