// Package workinput 定义需要跨执行与持久队列边界保真的中立工作输入。
package workinput

// Input 保存一次工作输入和有序能力引用。
type Input struct {
	// SubmissionID 是耐久调用者稳定复用的本次输入身份，空值不启用续发去重。
	SubmissionID string `json:"submissionId,omitempty"`
	// Actor 是产生本次输入的中立参与者标识。
	Actor string `json:"actor"`
	// Content 是本次需要执行的完整文本。
	Content string `json:"content"`
	// Attachments 是需要物化到受管工作区的附件。
	Attachments []Attachment `json:"attachments,omitempty"`
	// CodeReferences 是整文件或代码片段引用。
	CodeReferences []CodeReference `json:"codeReferences,omitempty"`
	// SkillIDs 是本轮明确选择的技能标识。
	SkillIDs []string `json:"skillIds,omitempty"`
	// ConnectorIDs 是本轮明确选择的连接器标识。
	ConnectorIDs []string `json:"connectorIds,omitempty"`
	// PluginIDs 是本轮明确选择的插件标识。
	PluginIDs []string `json:"pluginIds,omitempty"`
	// ContextInstructions 是入口冻结的执行说明，随耐久队列保真，不进入用户消息或标题。
	ContextInstructions []Instruction `json:"contextInstructions,omitempty"`
}

// Instruction 是由可信入口提供的中立执行上下文，不允许外部 API 直接提交。
type Instruction struct {
	// Name 是本轮虚拟说明文件的稳定名称。
	Name string `json:"name"`
	// Content 是只提供给智能体执行的说明正文。
	Content string `json:"content"`
}

// CodeReference 是提交前等待路径和可选行号校验的内容引用。
type CodeReference struct {
	// Path 是工作区相对路径或桌面绝对来源路径。
	Path string `json:"path"`
	// StartLine 是选区起始的一基行号，整文件引用为零。
	StartLine int `json:"startLine,omitempty"`
	// EndLine 是选区结束的一基行号，整文件引用为零。
	EndLine int `json:"endLine,omitempty"`
	// Content 是发送时冻结的选中文本，整文件引用为空。
	Content string `json:"content,omitempty"`
}

// Attachment 是一份等待后端权威校验的本地文件路径。
type Attachment struct {
	// SourcePath 是桌面文件选择器返回的本地文件路径。
	SourcePath string `json:"sourcePath"`
}
