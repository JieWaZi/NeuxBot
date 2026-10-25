package constants

//go:generate toolx gen enum IntegrationCapability

// IntegrationCapability 表示明确授予集成主体的操作能力。
type IntegrationCapability string

const (
	// IntegrationCapabilityRetrieve 沿用 WeKnora 的读取与检索能力。
	IntegrationCapabilityRetrieve IntegrationCapability = "retrieve"
	// IntegrationCapabilityIngest 沿用 WeKnora 的内容上传与编辑能力。
	IntegrationCapabilityIngest IntegrationCapability = "ingest"
	// IntegrationCapabilityOrganize 允许受控整理，不开放任意编辑。
	IntegrationCapabilityOrganize IntegrationCapability = "organize"
	// IntegrationCapabilityTaskInvoke 允许调用选定自动任务及上传受管输入。
	IntegrationCapabilityTaskInvoke IntegrationCapability = "automatic_task.invoke"
	// IntegrationCapabilityTaskResults 允许读取本主体发起的运行及受管产出。
	IntegrationCapabilityTaskResults IntegrationCapability = "automatic_task.results"
)
