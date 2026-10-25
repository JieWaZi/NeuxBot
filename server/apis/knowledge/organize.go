package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	service "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 整理知识原件
type Organize struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body OrganizeInput `in:"body"`
}

// 整理知识请求
type OrganizeInput struct {
	// 重建知识
	Rebuild bool `json:"rebuild,omitempty"`

	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 来源版本
	SourceVersionIDs []string `json:"sourceVersionIds"`
	// 智能体
	RuntimeID string `json:"runtimeId"`
	// 账号标识
	AccountID string `json:"accountId"`
	// 模型
	Model string `json:"model"`
	// 整理消息
	Message string `json:"message"`
}

// 整理接纳响应
type OrganizeResponse struct {
	// 接纳回执
	Receipt *Receipt `json:"receipt"`
	// 模型选择
	Configuration TaskConfiguration `json:"configuration"`
	// 无新增来源
	Noop bool `json:"noop"`
}

func (o *Organize) Path() string { return "/libraries/:libraryId/organize" }
func (o *Organize) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	r, s, err := global.KnowledgeService.Organize(bridge.LocalUserActivity(ctx), service.OrganizeCommand{Rebuild: o.Body.Rebuild, RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceVersionIDs: o.Body.SourceVersionIDs, Message: o.Body.Message, Configuration: models.KnowledgeTaskConfiguration{ID: o.Body.RequestID, RuntimeID: o.Body.RuntimeID, AccountID: o.Body.AccountID, Model: o.Body.Model}})
	if err != nil {
		return nil, err
	}
	response := OrganizeResponse{Configuration: taskConfigurationDTO(s), Noop: r == nil || r.State == "no-op"}
	if r != nil {
		receipt := receiptDTO(r)
		response.Receipt = &receipt
	}
	return response, nil
}
