package system

import (
	"runtime"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 版本响应
type VersionResponse struct {
	// 名称
	Name string `json:"name"`
	// 版本号
	Version string `json:"version"`
	// Go 版本
	GoVersion string `json:"goVersion"`
	// 运行平台
	Platform string `json:"platform"`
}

// 获取版本
type GetVersion struct {
	ginx.MethodGet
}

// Path 返回版本接口路径。
func (g *GetVersion) Path() string {
	return "/version"
}

// Output 返回版本信息。
func (g *GetVersion) Output(*gin.Context) (interface{}, error) {
	return VersionResponse{
		Name:      constants.ApplicationName,
		Version:   constants.ApplicationVersion,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}, nil
}
