// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&Project{})
	dbhelper.RegisterTable(&ProjectSourceBinding{})
}

// Project 保存一个伙伴独立拥有的项目配置。
type Project struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// WakerID 是拥有该项目的伙伴标识。
	WakerID string `gorm:"column:waker_id;type:varchar(32);not null;uniqueIndex:idx_projects_waker_name,priority:1;index" json:"wakerId"`
	// Name 是所属伙伴内唯一的项目展示名称。
	Name string `gorm:"column:name;type:varchar(80);not null;uniqueIndex:idx_projects_waker_name,priority:2" json:"name"`
	// Brief 是只用于界面展示的项目简介。
	Brief string `gorm:"column:brief;type:text;not null;default:''" json:"brief"`
	// CreatedAt 是记录创建时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是项目配置最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli;index" json:"updatedAt"`
}

// TableName 返回项目模型使用的表名。
func (Project) TableName() string {
	return "projects"
}

// ProjectSourceBinding 保存项目与可复用本地目录索引的有序关联。
type ProjectSourceBinding struct {
	// ProjectID 是关联所属项目标识。
	ProjectID string `gorm:"column:project_id;type:varchar(32);primaryKey" json:"projectId"`
	// ProjectSourceID 是关联的本地目录索引标识。
	ProjectSourceID string `gorm:"column:project_source_id;type:varchar(32);primaryKey" json:"projectSourceId"`
	// Position 是目录在项目详情中的稳定展示顺序。
	Position int `gorm:"column:position;not null" json:"position"`
	// LoadInstructions 表示新建对话时是否读取该目录根部的 Runtime 指令文件。
	LoadInstructions bool `gorm:"column:load_instructions;not null" json:"loadInstructions"`
	// IsDefault 表示该目录是否为项目唯一的默认目录。
	IsDefault bool `gorm:"column:is_default;not null;default:false" json:"isDefault"`
	// CreatedAt 是关联建立时的 Unix 毫秒时间。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回项目目录关联模型使用的表名。
func (ProjectSourceBinding) TableName() string {
	return "project_source_bindings"
}

// ProjectDirectory 聚合项目与一个本地目录之间的配置。
type ProjectDirectory struct {
	// Source 是用户显式授权的真实本地目录。
	Source ProjectSource
	// LoadInstructions 表示新建对话时是否读取该目录根部的 Runtime 指令文件。
	LoadInstructions bool
	// IsDefault 表示该目录是否为项目唯一的默认目录。
	IsDefault bool
}
