package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询工作文件
type StatFile struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `in:"query" name:"relativePath" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
}

// Path 返回工作文件元数据地址。
func (*StatFile) Path() string { return "/stat" }

// Output 返回文件版本，缺失时返回空值供编辑器展示删除状态。
func (g *StatFile) Output(ctx *gin.Context) (interface{}, error) {
	resource, _, err := files.Read(middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.RelativePath)
	if err == status_error.WorkspaceResourceNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return resource, nil
}
