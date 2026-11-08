// Package resourcecategory 规范化用户输入的多选资源分类。
package resourcecategory

import (
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Normalize 清理空白、移除空值与重复名称，保留用户顺序与大小写。
func Normalize(names []string) ([]string, error) {
	if len(names) > constants.ResourceCategoryMaximumCount {
		return nil, status_error.BadRequest
	}
	result := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, raw := range names {
		name := util.NormalizeName(raw)
		if utf8.RuneCountInString(name) > constants.ResourceCategoryMaximumNameLength {
			return nil, status_error.BadRequest
		}
		if name != "" && !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result, nil
}
