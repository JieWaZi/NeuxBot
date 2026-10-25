// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&ProjectSource{})
}

// ProjectSource 保存用户显式授权的本地项目目录索引。
type ProjectSource struct {
	// ID 是由规范化真实路径稳定派生的 32 位标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是文件系统目录名生成的展示名称。
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	// Path 是经过绝对化和符号链接解析的真实本地目录。
	Path string `gorm:"column:path;type:text;not null;uniqueIndex" json:"path"`
	// CreatedAt 是记录创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是记录最近确认目录有效时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回项目来源模型使用的表名。
func (ProjectSource) TableName() string {
	return "project_sources"
}
