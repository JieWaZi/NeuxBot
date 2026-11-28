package constants

//go:generate toolx gen enum ExecutionAdmission

// ExecutionAdmission 表示 Execution 何时进入权威事实源。
type ExecutionAdmission string

const (
	// ExecutionAdmissionCommitOnRuntimeAccept 表示 Runtime 接受调用后再持久化首批事实。
	ExecutionAdmissionCommitOnRuntimeAccept ExecutionAdmission = "commit_on_runtime_accept"
	// ExecutionAdmissionCommitBeforeDispatch 表示先持久化事实再派发 Runtime。
	ExecutionAdmissionCommitBeforeDispatch ExecutionAdmission = "commit_before_dispatch"
	// ExecutionAdmissionCommitAtQueuedNativeGate 表示持久队列在原生发送门同时记录执行和实际输入。
	ExecutionAdmissionCommitAtQueuedNativeGate ExecutionAdmission = "commit_at_queued_native_gate"
)
