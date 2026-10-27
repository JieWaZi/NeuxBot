package contract

import "context"

// ResourceBinding 是已物化、由租约保护的资源。
type ResourceBinding struct {
	ID   string // ID 是与请求匹配的稳定资源身份。
	Path string // Path 是物化后的本地路径。
}

// ResourceRequest 请求一次会话所需的资源快照。
type ResourceRequest struct {
	Invocation    InvocationRef  // Invocation 是本次准备调用。
	Workspace     WorkspaceScope // Workspace 是有效授权范围。
	Contributions Contributions  // Contributions 是需要物化的资源描述。
}

// ResourceLease 必须保护资源至会话关闭，Close 须遵守 Context。
// Close 失败时租约仍由 Runtime 拥有；重试只调用尚未成功释放的租约。
type ResourceLease interface {
	Resources() []ResourceBinding
	Close(context.Context) error
}

// ResourceProvider 只物化已授权描述，不扩张权限。
type ResourceProvider interface {
	Acquire(context.Context, ResourceRequest) (ResourceLease, error)
}

// Credential 只在启动边界传递，不能写入摘要、日志或检查点。
type Credential struct {
	Environment map[string]string // Environment 是仅供原生进程的秘密环境。
}

// CredentialRequest 通过稳定身份申请凭据租约。
type CredentialRequest struct {
	Ref        string // Ref 是宿主凭据引用。
	Subject    string // Subject 是固定账号主体。
	Generation string // Generation 是固定凭据代际。
}

// CredentialLease 保护正在使用的秘密，Close 须遵守 Context。
// Close 成功表示租约已释放，失败时须允许 Runtime 保留所有权并重试。
type CredentialLease interface {
	Credential() Credential
	Close(context.Context) error
}

// CredentialProvider 由宿主拥有账号、刷新和排队权威。
type CredentialProvider interface {
	Acquire(context.Context, CredentialRequest) (CredentialLease, error)
}
