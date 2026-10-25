package integration

// Route 是实际发布登记导出的生成投影，不负责注册或授权。
type Route struct {
	// Method 是实际 HTTP 方法。
	Method string `json:"method"`
	// Path 是实际路由模板。
	Path string `json:"path"`
	// OperationID 是同一 Operator 的生成标识。
	OperationID string `json:"operationId"`
}
