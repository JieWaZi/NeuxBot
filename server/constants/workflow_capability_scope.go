package constants

//go:generate toolx gen enum WorkflowCapabilityScope

// WorkflowCapabilityScope 是服务端签发的工作流能力用途，不接受模型通过查询参数切换。
type WorkflowCapabilityScope string

const (
	// WorkflowCapabilityScopeResult 只允许当前节点尝试查询格式和提交结果。
	WorkflowCapabilityScopeResult WorkflowCapabilityScope = "result"
	// WorkflowCapabilityScopeConsumer 只允许当前执行获准的工作流调用。
	WorkflowCapabilityScopeConsumer WorkflowCapabilityScope = "consumer"
)
