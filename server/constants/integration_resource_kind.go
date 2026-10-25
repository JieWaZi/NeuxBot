package constants

//go:generate toolx gen enum IntegrationResourceKind

// IntegrationResourceKind 区分集成授权的两类权威资源。
type IntegrationResourceKind string

const (
	// IntegrationResourceKindKnowledge 表示本机权威知识库。
	IntegrationResourceKindKnowledge IntegrationResourceKind = "knowledge"
	// IntegrationResourceKindAutomaticTask 表示可通过 API 调用的自动任务。
	IntegrationResourceKindAutomaticTask IntegrationResourceKind = "automatic_task"
)
