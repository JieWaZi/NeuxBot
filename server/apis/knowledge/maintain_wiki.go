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

// 整理Wiki请求
type MaintainWikiInput struct {
	RequestID string `json:"requestId" validate:"required"` // 请求标识
	RuntimeID string `json:"runtimeId"`                     // 智能体
	AccountID string `json:"accountId"`                     // 账号标识
	Model     string `json:"model"`                         // 模型
	Reasoning string `json:"reasoning" validate:"max=80"`   // 推理强度
	Message   string `json:"message" validate:"max=65536"`  // 整理要求
}

// 整理Wiki
type MaintainWiki struct {
	ginx.MethodPost
	LibraryID string            `in:"path" name:"libraryId" validate:"required"` // 知识库标识
	Body      MaintainWikiInput `in:"body"`                                      // 请求体
}

// Path 返回独立 Wiki 维护任务的接纳入口。
func (o *MaintainWiki) Path() string { return "/libraries/:libraryId/maintain" }

// Output 冻结模型配置并接纳独立 ACP 任务，不创建伙伴或聊天。
func (o *MaintainWiki) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	receipt, configuration, err := global.KnowledgeService.Organize(bridge.LocalUserActivity(ctx), service.OrganizeCommand{Maintenance: true, RequestID: o.Body.RequestID, LibraryID: o.LibraryID, Message: o.Body.Message, Configuration: models.KnowledgeTaskConfiguration{ID: o.Body.RequestID, RuntimeID: o.Body.RuntimeID, AccountID: o.Body.AccountID, Model: o.Body.Model, Reasoning: o.Body.Reasoning}})
	if err != nil {
		return nil, err
	}
	r := receiptDTO(receipt)
	return OrganizeResponse{Receipt: &r, Configuration: taskConfigurationDTO(configuration)}, nil
}

func init() { Router.Register(&MaintainWiki{}) }
