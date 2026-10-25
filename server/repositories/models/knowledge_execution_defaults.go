package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&LibraryExecutionDefaults{})
	dbhelper.RegisterTable(&LibraryExecutionDefaultsMutation{})
}

// LibraryExecutionDefaults 是本机知识库唯一的执行选择偏好，不属于通用知识核心配置。
type LibraryExecutionDefaults struct {
	// LibraryID 是唯一所属本地知识库。
	LibraryID string `gorm:"primaryKey" json:"libraryId"`
	// Version 是本机偏好的乐观并发版本，未设置时为零。
	Version int64 `json:"version"`
	// RuntimeID 是选定智能体，不包含程序路径。
	RuntimeID string `json:"runtimeId"`
	// AccountID 是既有账号引用，不包含认证资料。
	AccountID string `json:"accountId"`
	// Model 是用户选择的模型标识。
	Model string `json:"model"`
	// Reasoning 是显式推理强度，留空时使用模型支持的最低档。
	Reasoning string `json:"reasoning,omitempty"`
	// UpdatedAt 是最近成功保存时间，单位毫秒。
	UpdatedAt int64 `json:"updatedAt"`
}

// TableName 固定库执行偏好的宿主持久表名。
func (LibraryExecutionDefaults) TableName() string { return "knowledge_library_execution_defaults" }

// LibraryExecutionDefaultsMutation 仅保存幂等回执，不能作为另一份可编辑配置。
type LibraryExecutionDefaultsMutation struct {
	// RequestID 是用户此次保存动作的稳定身份。
	RequestID string `gorm:"primaryKey"`
	// LibraryID 限定回执所属知识库。
	LibraryID string `gorm:"index"`
	// Digest 固定此次保存的原请求内容。
	Digest string
	// Snapshot 是该次已提交结果，后续偏好改变不改写回执。
	Snapshot LibraryExecutionDefaults `gorm:"serializer:json;type:text"`
}

// TableName 固定执行偏好幂等回执的宿主持久表名。
func (LibraryExecutionDefaultsMutation) TableName() string {
	return "knowledge_library_execution_defaults_mutations"
}
