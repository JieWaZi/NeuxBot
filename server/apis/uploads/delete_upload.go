package uploads

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除暂存上传
type DeleteUpload struct {
	// ginx.MethodDelete 声明接口方法。
	ginx.MethodDelete
	// 上传引用
	SourcePath string `in:"query" name:"sourcePath" validate:"required,max=1024"`
}

func (*DeleteUpload) Path() string { return "" }
func (op *DeleteUpload) Output(ctx *gin.Context) (interface{}, error) {
	if global.Uploads == nil {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	if err := global.Uploads.Discard(middleware.AuthSubject(ctx), op.SourcePath); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
