package work_board

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListWorkBoardExecutors 搜索伙伴、团队和工作流配置，结果有界。
type ListWorkBoardExecutors struct {
	// MethodGet 声明只读 HTTP 方法。
	ginx.MethodGet
	// Query 是执行对象名称搜索，不搜索历史工作正文。
	Query string `in:"query" name:"query" validate:"max=100"`
}

// WorkBoardExecutorsResponse 返回最多五十个名称匹配的执行对象。
type WorkBoardExecutorsResponse struct {
	// Resources 是原配置对象，不含工作历史。
	Resources []models.WorkBoardExecutor `json:"resources"`
}

// Path 返回执行对象搜索路径。
func (*ListWorkBoardExecutors) Path() string { return "/executors" }

// Output 在查询预算内搜索原配置。
func (d *ListWorkBoardExecutors) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := queryContext(ctx)
	defer cancel()
	values, err := global.WorkBoardControllerManager.Executors(request, d.Query)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if values == nil {
		values = []models.WorkBoardExecutor{}
	}
	return WorkBoardExecutorsResponse{Resources: values}, nil
}
