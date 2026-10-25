// Package capability 保存工作流业务场景身份，CLI 契约由 agentcapability/contract 唯一维护。
package capability

import "github.com/JieWaZi/NeuxBot/server/constants"

// Input 是稳定工具面的结构化动作输入。
type Input struct {
	// InputFileRoots 仅由服务端冻结文件适配提供，不接纳模型参数。
	InputFileRoots []string `json:"-"`
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
	// Source 是完整原始源码。
	Source string `json:"source,omitempty"`
	// SDKVersion 是创作选择的契约版本。
	SDKVersion string `json:"sdkVersion,omitempty"`
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
	// Status 是批注处理状态过滤条件。
	Status string `json:"status,omitempty"`
	// BaseRevisionID 是用户要求查看的批注基准。
	BaseRevisionID string `json:"baseRevisionId,omitempty"`
	// SubmissionID 是结构化候选结果的调用身份。
	SubmissionID string `json:"submissionId,omitempty"`
	// ResolvedAnnotationIDs 是创作声明处理的批注。
	ResolvedAnnotationIDs []string `json:"resolvedAnnotationIds,omitempty"`
}

// Scope 是服务端从 URL 和认证头提取的能力范围。
type Scope struct {
	// Key 是服务端校验的创作会话或效果身份。
	Key string
	// Token 是需要 constant-time 校验的 bearer 令牌。
	Token string
	// Kind 是由服务端端点和持久凭据共同校验的能力用途。
	Kind constants.WorkflowCapabilityScope
}

// Allows 只实施业务角色授权，不复制 CLI 输入契约。
func Allows(scope constants.WorkflowCapabilityScope, input Input) bool {
	if input.Action == constants.WorkflowActionQuery {
		if input.Type == constants.WorkflowQueryCapabilities {
			return scope == constants.WorkflowCapabilityScopeCreation || scope == constants.WorkflowCapabilityScopeConsumer || scope == constants.WorkflowCapabilityScopeResult
		}
		switch scope {
		case constants.WorkflowCapabilityScopeCreation:
			return input.Type == constants.WorkflowQuerySDK || input.Type == constants.WorkflowQueryExamples || input.Type == constants.WorkflowQueryResources || input.Type == constants.WorkflowQueryAuthoring
		case constants.WorkflowCapabilityScopeConsumer:
			return input.Type == constants.WorkflowQueryWorkflows
		case constants.WorkflowCapabilityScopeResult:
			return input.Type == constants.WorkflowQueryResultSchema
		}
	}
	switch scope {
	case constants.WorkflowCapabilityScopeCreation:
		switch input.Action {
		case constants.WorkflowActionSourceGet, constants.WorkflowActionSourceValidate, constants.WorkflowActionSourceUpsert, constants.WorkflowActionAnnotationsList, constants.WorkflowActionRunsGet:
			return true
		}
	case constants.WorkflowCapabilityScopeConsumer:
		switch input.Action {
		case constants.WorkflowActionRunStart, constants.WorkflowActionRunGet, constants.WorkflowActionRunCancel:
			return true
		}
	case constants.WorkflowCapabilityScopeResult:
		return input.Action == constants.WorkflowActionResultSubmit
	}
	return false
}

// Capabilities 只用于内部授权探测，不向模型发布第二份命令目录。
func Capabilities(scope constants.WorkflowCapabilityScope) map[string]any {
	return map[string]any{"scope": scope}
}
