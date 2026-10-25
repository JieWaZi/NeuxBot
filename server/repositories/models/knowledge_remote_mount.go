package models

import "github.com/shrewx/ginx/pkg/dbhelper"

func init() {
	dbhelper.RegisterTable(&KnowledgeRemoteMount{})
	dbhelper.RegisterTable(&KnowledgeRemoteMountMutation{})
}

// KnowledgeRemoteMount 只保存用户明确指定的远端库入口，不枚举 Key 的全部可见库。
type KnowledgeRemoteMount struct {
	// ID 是本机挂载身份，删除后不会被同 ID 远端自动接管。
	ID string `gorm:"primaryKey" json:"id"`
	// ConnectionID 固定实际连接与凭据身份。
	ConnectionID string `gorm:"uniqueIndex:idx_remote_mount_target,priority:1" json:"connectionId"`
	// KnowledgeBaseID 是服务器已核对的库身份。
	KnowledgeBaseID string `gorm:"uniqueIndex:idx_remote_mount_target,priority:2" json:"knowledgeBaseId"`
	// Revision 是本机入口的条件修改版本。
	Revision int64 `json:"revision"`
}

func (KnowledgeRemoteMount) TableName() string { return "knowledge_remote_mounts" }

// KnowledgeRemoteMountMutation 只登记本机添加意图和原目标，不包含秘密或远端内容。
type KnowledgeRemoteMountMutation struct {
	// RequestID 是添加动作的稳定身份。
	RequestID string `gorm:"primaryKey" json:"-"`
	// Digest 绑定原 URL、Key 摘要和库身份。
	Digest string `json:"-"`
	// MountID 是首次成功提交的挂载。
	MountID string `json:"mountId"`
}

func (KnowledgeRemoteMountMutation) TableName() string { return "knowledge_remote_mount_mutations" }
