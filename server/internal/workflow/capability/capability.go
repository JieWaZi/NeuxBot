// Package capability 保存工作流业务场景身份，CLI 契约由 agent/capability 唯一维护。
package capability

import (
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
)

// Input 是稳定工具面的结构化动作输入。
type Input struct {
	// StagedInputs 由宿主签名文件范围完成一次复制，外部参数无法填写。
	StagedInputs *workflowfiles.StagedInputs `json:"-"`
	// Action 是统一工具中的操作或按需查询动作。
	Action string `json:"action"`
	// Type 只选择本次能力允许的查询内容。
	Type string `json:"type,omitempty"`
	// Topic 选择 SDK 或示例的具体主题。
	Topic string `json:"topic,omitempty"`
	// Primitives 是准备使用的原生调用，由工具据此组合最小资料。
	Primitives []string `json:"primitives,omitempty"`
	// Features 是需要额外 Schema、展示或交付规则的能力。
	Features []string `json:"features,omitempty"`
	// Language 用于查询实际 Function 解释器可用性。
	Language string `json:"language,omitempty"`
	// IncludeManifest 只在显式请求时返回完整校验投影。
	IncludeManifest bool `json:"includeManifest,omitempty"`
	// Data 是结构化结果或动作数据。
	Data any `json:"data,omitempty"`
	// RequestID 是幂等修改请求。
	RequestID string `json:"requestId,omitempty"`
	// ExpectedRevisionID 是创作读取的当前版本。
	ExpectedRevisionID string `json:"expectedRevisionId,omitempty"`
	// BaseSourceDigest 是精确补丁所依赖的源码摘要。
	BaseSourceDigest string `json:"baseSourceDigest,omitempty"`
	// Patches 是顺序执行的唯一匹配替换。
	Patches []SourcePatch `json:"patches,omitempty"`
	// Source 是完整原始源码。
	Source string `json:"source,omitempty"`
	// SDK 是创作源码使用的契约身份。
	SDK string `json:"sdk,omitempty"`
	// RunID 是创作查询的实际运行。
	RunID string `json:"runId,omitempty"`
	// WorkflowID 是使用者白名单内的运行目标。
	WorkflowID string `json:"workflowId,omitempty"`
	// RevisionID 是同一定义内可选的固定版本。
	RevisionID string `json:"revisionId,omitempty"`
	// Args 是只通过冻结输入 Schema 校验的运行参数。
	Args any `json:"args,omitempty"`
	// Search 是使用者目录的字面搜索条件。
	Search string `json:"search,omitempty"`
	// Kind 是创作目录中请求的资源类别。
	Kind string `json:"kind,omitempty"`
	// Cursor 是目录的后续窗口。
	Cursor string `json:"cursor,omitempty"`
	// PageParams 保留已验证的运行目录分页参数及显式参数存在性。
	PageParams map[string]any `json:"-"`
	// Status 是批注处理状态过滤条件。
	Status string `json:"status,omitempty"`
	// BaseRevisionID 是用户要求查看的批注基准。
	BaseRevisionID string `json:"baseRevisionId,omitempty"`
	// SubmissionID 是结构化候选结果的调用身份。
	SubmissionID string `json:"submissionId,omitempty"`
	// ResolvedAnnotationIDs 是创作声明处理的批注。
	ResolvedAnnotationIDs []string `json:"resolvedAnnotationIds,omitempty"`
}

// Scope 是 Core 装配 botcli 操作时固定的工作流业务范围。
type Scope struct {
	// Key 是创作会话、运行调用回合或节点效果身份。
	Key string
	// ExecutionID 是创作或本地对话固定的执行标识。
	ExecutionID string
	// LocalChatID 标识使用共享工作流工具的本地交互对话。
	LocalChatID string
	// DefaultWorkflowID 是界面当前工作流或用户引用的默认目标。
	DefaultWorkflowID string
	// Selection 固定用户通过聊天引用选择的工作流版本。
	Selection *workinput.WorkflowReference
	// SavedRevisions 仅在本次能力生命周期内记录已返回保存回执的版本。
	SavedRevisions *sync.Map
	// Token 是节点结果提交使用的尝试凭证。
	Token string
	// Kind 是服务端签发的能力用途。
	Kind constants.WorkflowCapabilityScope
}

// Allows 只实施业务角色授权，不复制 CLI 输入契约。
func Allows(scope constants.WorkflowCapabilityScope, input Input) bool {
	if input.Action == constants.WorkflowActionQuery {
		switch scope {
		case constants.WorkflowCapabilityScopeConsumer:
			return input.Type == constants.WorkflowQueryWorkflows
		case constants.WorkflowCapabilityScopeResult:
			return input.Type == constants.WorkflowQueryResultSchema
		}
	}
	switch scope {
	case constants.WorkflowCapabilityScopeConsumer:
		switch input.Action {
		case constants.WorkflowActionRunStart, constants.WorkflowActionRunGet, constants.WorkflowActionRunCancel, constants.WorkflowActionRunList, constants.WorkflowActionRunRepeat:
			return true
		}
	case constants.WorkflowCapabilityScopeResult:
		return input.Action == constants.WorkflowActionResultSubmit
	}
	return false
}

// SourcePatch 是完整源码的精确文本补丁。
type SourcePatch struct {
	// Search 是必须唯一匹配的原始字节。
	Search string `json:"search"`
	// Replace 是用于替换的新字节。
	Replace string `json:"replace"`
}
