package workspace

import (
	"encoding/base64"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ImportFileRequest 是工作区操作参数。
type ImportFileRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 目标目录
	DirectoryPath string `json:"directoryPath"`
	// 文件名称
	Name string `json:"name" validate:"required"`
	// 文件内容
	BytesBase64 string `json:"bytesBase64"`
}

// ImportFile 处理工作区操作。
type ImportFile struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body ImportFileRequest `in:"body"`
}

// Path 返回操作地址。
func (*ImportFile) Path() string { return "/import" }

// Output 在主体范围内执行操作。
func (g *ImportFile) Output(ctx *gin.Context) (interface{}, error) {
	bytes, err := base64.StdEncoding.DecodeString(g.Body.BytesBase64)
	if err != nil {
		return nil, ginx.WithStack(status_error.WorkspacePathInvalid)
	}
	relative, err := files.Import(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.DirectoryPath, g.Body.Name, bytes)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return PathResponse{RelativePath: relative}, nil
}
