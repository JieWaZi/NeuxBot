package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询原集成请求回执
type PublishedGetReceipt struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
	// 原请求标识
	RequestID string `in:"path" name:"requestId" validate:"required,max=200"`
}

// Path 是按原请求恢复的能力扩展。
func (*PublishedGetReceipt) Path() string {
	return "/knowledge-bases/:id/integration-receipts/:requestId"
}

// Output 从当前主体与库派生原身份，不接受他人内部回执 ID 冒认。
func (o *PublishedGetReceipt) Output(ctx *gin.Context) (interface{}, error) {
	originalID, requestID, err := scopedRequest(ctx, o.LibraryID, o.RequestID)
	if err != nil {
		return nil, err
	}
	receipt, err := dependencies(ctx).engine.GetReceipt(ctx.Request.Context(), requestID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return publicReceipt(receipt, originalID), nil
}
