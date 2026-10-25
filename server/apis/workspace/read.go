package workspace

import (
	"encoding/base64"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文件读取响应
type ReadResponse struct {
	// 文件信息
	Resource workspacefiles.Resource `json:"resource"`
	// 文件字节
	BytesBase64 string `json:"bytesBase64"`
}

// 读取工作文件
type ReadFile struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `in:"query" name:"relativePath" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
}

// Path 返回工作文件读取地址。
func (*ReadFile) Path() string { return "/read" }

// Output 返回有界的文件原字节和同一次读取的版本。
func (g *ReadFile) Output(ctx *gin.Context) (interface{}, error) {
	resource, bytes, err := files.Read(middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.RelativePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ReadResponse{Resource: resource, BytesBase64: base64.StdEncoding.EncodeToString(bytes)}, nil
}
