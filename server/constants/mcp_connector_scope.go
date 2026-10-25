package constants

//go:generate toolx gen enum MCPConnectorScope

// MCPConnectorScope 表示 MCP 连接器在 NeuxBot 中的来源范围。
type MCPConnectorScope string

const (
	// MCPConnectorScopeUnspecified 表示数据库或框架产生的空范围。
	MCPConnectorScopeUnspecified MCPConnectorScope = ""
	// MCPConnectorScopeBuiltIn 表示随 NeuxBot 提供的内置连接器。
	MCPConnectorScopeBuiltIn MCPConnectorScope = "built_in"
	// MCPConnectorScopePublic 表示全部伙伴可见的用户连接器。
	MCPConnectorScopePublic MCPConnectorScope = "public"
	// MCPConnectorScopePrivate 表示只属于一个伙伴的连接器。
	MCPConnectorScopePrivate MCPConnectorScope = "private"
)
