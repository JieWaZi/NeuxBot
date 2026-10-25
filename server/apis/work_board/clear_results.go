package work_board

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 清空待查看结果
type ClearWorkResults struct {
	// 写入方法
	ginx.MethodPost
}

// Path 返回用户主动清空全部待查看结果的确认入口。
func (*ClearWorkResults) Path() string { return "/results/clear" }

// Output 确认原子操作执行时的全部待查看版本，保留原结果与后续新交付。
func (*ClearWorkResults) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := queryContext(ctx)
	defer cancel()
	if err := global.NotificationService.ClearUnread(request); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
