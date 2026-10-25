// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&SkillPackage{})
	dbhelper.RegisterTable(&SkillVersion{})
	dbhelper.RegisterTable(&WakerSkill{})
}

// SkillPackage 保存一个技能包的共享元数据和来源信息。
type SkillPackage struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是技能包展示名称。
	Name string `gorm:"column:name;type:varchar(120);not null" json:"name"`
	// Description 是技能包的简短说明。
	Description string `gorm:"column:description;type:text;not null;default:''" json:"description"`
	// Scope 是技能包的公开或私有范围。
	Scope constants.SkillScope `gorm:"column:scope;type:varchar(16);not null;index" json:"scope"`
	// Source 是技能包的导入来源。
	Source constants.SkillSource `gorm:"column:source;type:varchar(16);not null;index" json:"source"`
	// OwnerWakerID 是私有技能所属伙伴，公开技能为空。
	OwnerWakerID string `gorm:"column:owner_waker_id;type:varchar(32);index" json:"ownerWakerId,omitempty"`
	// BuiltIn 表示技能是否随 NeuxBot 提供且不可卸载。
	BuiltIn bool `gorm:"column:built_in;not null;default:false;index" json:"builtIn"`
	// GitURL 是 Git 来源地址。
	GitURL string `gorm:"column:git_url;type:text;not null;default:''" json:"gitUrl,omitempty"`
	// GitRef 是 Git 来源使用的分支、标签或提交引用。
	GitRef string `gorm:"column:git_ref;type:varchar(160);not null;default:''" json:"gitRef,omitempty"`
	// GitSubpath 是该技能目录相对于 Git 仓库根的路径，供单技能更新重新定位候选。
	GitSubpath string `gorm:"column:git_subpath;type:text;not null;default:''" json:"gitSubpath,omitempty"`
	// CurrentVersionID 是当前启用版本的标识。
	CurrentVersionID string `gorm:"column:current_version_id;type:varchar(32);not null;default:''" json:"currentVersionId"`
	// CreatedAt 是创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近更新时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回技能包模型使用的表名。
func (SkillPackage) TableName() string { return "skill_packages" }

// SkillVersion 保存不可变的技能版本元数据。
type SkillVersion struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// SkillPackageID 是所属技能包标识。
	SkillPackageID string `gorm:"column:skill_package_id;type:varchar(32);not null;uniqueIndex:idx_skill_version_package_version,priority:1" json:"skillPackageId"`
	// Version 是 NeuxBot 管理的 SemVer 版本号。
	Version string `gorm:"column:version;type:varchar(40);not null;uniqueIndex:idx_skill_version_package_version,priority:2" json:"version"`
	// Name 是该不可变版本声明的技能名称。
	Name string `gorm:"column:name;type:varchar(120);not null;default:''" json:"name"`
	// Description 是该不可变版本声明的技能说明。
	Description string `gorm:"column:description;type:text;not null;default:''" json:"description"`
	// Operation 是产生该版本的操作说明。
	Operation string `gorm:"column:operation;type:varchar(32);not null;default:'import'" json:"operation"`
	// Notes 是用户可见的版本备注。
	Notes string `gorm:"column:notes;type:text;not null;default:''" json:"notes"`
	// ContentPath 是应用数据目录内完整技能包的相对路径。
	ContentPath string `gorm:"column:content_path;type:text;not null" json:"contentPath"`
	// ContentDigest 是完整技能包内容的 SHA-256 摘要。
	ContentDigest string `gorm:"column:content_digest;type:varchar(64);not null" json:"contentDigest"`
	// CreatedAt 是创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回技能版本模型使用的表名。
func (SkillVersion) TableName() string { return "skill_versions" }

// WakerSkill 保存伙伴对技能包的独立启停关系。
type WakerSkill struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// WakerID 是伙伴标识。
	WakerID string `gorm:"column:waker_id;type:varchar(32);not null;uniqueIndex:idx_waker_skill;index:idx_skill_installed_partners,priority:2" json:"wakerId"`
	// SkillPackageID 是技能包标识。
	SkillPackageID string `gorm:"column:skill_package_id;type:varchar(32);not null;uniqueIndex:idx_waker_skill;index:idx_skill_installed_partners,priority:1" json:"skillPackageId"`
	// Enabled 表示该伙伴是否在后续执行中使用技能。
	Enabled bool `gorm:"column:enabled;not null;default:false" json:"enabled"`
	// CreatedAt 是创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近更新时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回伙伴技能关系模型使用的表名。
func (WakerSkill) TableName() string { return "waker_skills" }

// SkillWithState 是面向伙伴查询时合并技能包和启用状态的结果。
type SkillWithState struct {
	// SkillPackage 是技能包元数据。
	SkillPackage SkillPackage `json:"skill"`
	// CurrentVersion 是技能包当前版本。
	CurrentVersion SkillVersion `json:"currentVersion"`
	// Enabled 表示当前伙伴是否启用该技能。
	Enabled bool `json:"enabled"`
}
