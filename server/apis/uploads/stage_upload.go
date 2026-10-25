package uploads

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	uploadstore "github.com/JieWaZi/NeuxBot/server/internal/uploads"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
)

// 上传结果
type UploadResponse struct {
	// 上传引用
	SourcePath string `json:"sourcePath"`
}

// 暂存上传字节
type StageUpload struct {
	// ginx.MethodPost 声明接口方法。
	ginx.MethodPost
	// 文件名称
	Name string `in:"query" name:"name" validate:"required,max=255"`
}

func (*StageUpload) Path() string { return "" }
func (op *StageUpload) Output(ctx *gin.Context) (interface{}, error) {
	if global.Uploads == nil {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return nil, nil
	}
	ref, err := global.Uploads.Stage(middleware.AuthLifetime(ctx), middleware.AuthSubject(ctx), op.Name, http.MaxBytesReader(ctx.Writer, ctx.Request.Body, uploadstore.MaximumBytes))
	if err != nil {
		return nil, status_error.BadRequest
	}
	return UploadResponse{SourcePath: ref}, nil
}
