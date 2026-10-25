package uploads

import (
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
	_, release, err := Claim(ctx, []string{op.SourcePath})
	if err != nil {
		return nil, err
	}
	release(true)
	return ginx.CommonSuccessResponse(), nil
}
