// Package constants 定义 NeuxBot 跨层共享的稳定枚举和状态常量。
package constants

//go:generate toolx gen enum ProfileDocumentKind

// ProfileDocumentKind 表示角色预设或伙伴档案中的标准文档种类。
type ProfileDocumentKind string

const (
	// ProfileDocumentKindUnspecified 表示数据库或框架产生的空文档种类。
	ProfileDocumentKindUnspecified ProfileDocumentKind = ""
	// ProfileDocumentKindIdentity 表示身份与擅长领域文档。
	ProfileDocumentKindIdentity ProfileDocumentKind = "identity"
	// ProfileDocumentKindPersona 表示工作风格文档。
	ProfileDocumentKindPersona ProfileDocumentKind = "persona"
	// ProfileDocumentKindPlaybook 表示工作方式文档。
	ProfileDocumentKindPlaybook ProfileDocumentKind = "playbook"
	// ProfileDocumentKindMemory 表示伙伴独立拥有的记忆文档。
	ProfileDocumentKindMemory ProfileDocumentKind = "memory"
)

// ProfileDocumentFileName 返回已知档案种类使用的稳定文件名。
func ProfileDocumentFileName(kind ProfileDocumentKind) (string, bool) {
	switch kind {
	case ProfileDocumentKindIdentity:
		return ProfileDocumentIdentityFileName, true
	case ProfileDocumentKindPersona:
		return ProfileDocumentPersonaFileName, true
	case ProfileDocumentKindPlaybook:
		return ProfileDocumentPlaybookFileName, true
	case ProfileDocumentKindMemory:
		return ProfileDocumentMemoryFileName, true
	default:
		return "", false
	}
}
