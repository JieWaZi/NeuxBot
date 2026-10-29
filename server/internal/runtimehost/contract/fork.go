package contract

// ForkSessionRequest 保存分叉所需的原生身份、位置及目标执行环境。
type ForkSessionRequest struct {
	// ReceiptDirectory 是宿主持有的非公开持久操作目录。
	ReceiptDirectory string
	// ReconcileOnly 仅查询已执行原生调用的结果，禁止创建第二个分支。
	ReconcileOnly bool
	// SourceSessionID 是来源的原生句柄，不能交给前端。
	SourceSessionID string
	// Position 是适配器在回合终态返回的不透明定位信息；空值表示末尾。
	Position string
	// SourceWorkspace 是原生会话当前绑定的工作区。
	SourceWorkspace string
	// Run 提供目标工作区、Runtime 配置与权限；不使用消息和历史字段。
	Run RunRequest
}

// ForkSessionResult 是已建立的独立原生分支。
type ForkSessionResult struct {
	// SessionID 是之后续聊和冷恢复使用的新原生句柄。
	SessionID string
	// Mode 是当前实际适配器宣告的分叉精度。
	Mode string
}
