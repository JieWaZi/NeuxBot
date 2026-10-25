package im

import "github.com/JieWaZi/NeuxBot/server/repositories/models"

// ListIMConnectionsResponse 连接列表响应。
type ListIMConnectionsResponse struct {
	// 资源列表
	Resources []models.IMConnection `json:"resources"`
}

// ListIMBindingsResponse 授权列表响应。
type ListIMBindingsResponse struct {
	// 资源列表
	Resources []models.IMBinding `json:"resources"`
	// 资源总数
	TotalSize int64 `json:"totalSize"`
}

// GetIMStatusResponse 运行策略响应。
type GetIMStatusResponse struct {
	// 维持运行
	KeepAwake bool `json:"keepAwake"`
}

// VerifyIMLoginRequest 验证码请求。
type VerifyIMLoginRequest struct {
	// 验证码
	Code string `json:"code" validate:"required,max=32"`
}

// ListIMConversationsResponse 对话列表响应。
type ListIMConversationsResponse struct {
	// 资源列表
	Resources []models.IMConversationLink `json:"resources"`
}
