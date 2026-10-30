package contract

import "context"

// ResourceBinding 是已物化、由租约保护的资源。
type ResourceBinding struct {
	ID   string         // ID 是与请求匹配的稳定资源身份。
	Path string         // Path 是物化后的技能目录。
	MCP  *MCPConnection // MCP 是仅交给原生启动边界的连接配置。
}

// MCPTransport 区分公开 MCP 的连接传输。
type MCPTransport string

const (
	MCPStdio MCPTransport = "stdio" // MCPStdio 通过本地命令连接。
	MCPHTTP  MCPTransport = "http"  // MCPHTTP 通过 Streamable HTTP 连接。
	MCPSSE   MCPTransport = "sse"   // MCPSSE 通过 SSE 连接。
)

// MCPConnection 是宿主租约保护的实际连接，不进入静态会话声明。
type MCPConnection struct {
	Transport   MCPTransport      // Transport 是本连接的实际传输。
	Command     string            // Command 是本地连接命令。
	Arguments   []string          // Arguments 是本地命令有序参数。
	Environment map[string]string // Environment 是仅供连接子进程使用的环境。
	URL         string            // URL 是远程连接地址。
	Headers     map[string]string // Headers 是仅供远程连接使用的请求头。
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

// Credential 只在启动边界传递，不能写入静态会话声明或日志。
type Credential struct {
	Environment map[string]string // Environment 是仅供原生进程的秘密环境。
}
