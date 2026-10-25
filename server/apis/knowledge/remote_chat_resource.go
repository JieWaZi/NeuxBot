package knowledge

import service "github.com/JieWaZi/NeuxBot/server/services/knowledge"

// 聊天远端整理请求
type RemoteChatInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 资源引用
	Ref ResourceRef `json:"ref"`
	// 服务器配置
	ProfileID string `json:"profileId" validate:"required,max=120"`
	// 所选附件
	Attachments []ChatOperationAttachment `json:"attachments" validate:"required,min=1,max=20,dive"`
}

// 聊天操作附件沿用桌面文件选择器与既有Chat附件物化，不接受模型工具调用。
type ChatOperationAttachment struct {
	// 用户所选文件
	SourcePath string `json:"sourcePath" validate:"required,max=4096"`
}

// 聊天远端知识操作只包含真实回执引用，不伪造聊天Execution。
type RemoteChatResult struct {
	// 资源引用
	Ref ResourceRef `json:"ref"`
	// 聊天标识
	ChatID string `json:"chatId"`
	// 操作标识
	RequestID string `json:"requestId"`
	// 接纳状态
	State string `json:"state"`
	// 待确认请求
	PendingRequestID string `json:"pendingRequestId"`
	// 可继续接纳
	CanResume bool `json:"canResume"`
	// 原始回执
	Receipt *Receipt `json:"receipt,omitempty"`
	// 远端任务
	Job *Job `json:"job,omitempty"`
}

func remoteChatDTO(v *service.RemoteChatOperation) RemoteChatResult {
	out := RemoteChatResult{Ref: ResourceRef{Backend: v.Ref.Backend, ConnectionID: v.Ref.ConnectionID, KnowledgeBaseID: v.Ref.KnowledgeBaseID}, ChatID: v.ChatID, RequestID: v.RequestID, State: v.State, PendingRequestID: v.PendingRequestID, CanResume: v.CanResume}
	if v.Receipt != nil {
		receipt := receiptDTO(v.Receipt)
		out.Receipt = &receipt
	}
	if v.Job != nil {
		job := jobDTO(v.Job)
		out.Job = &job
	}
	return out
}
