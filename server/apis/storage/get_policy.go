package storage

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetStoragePolicy 读取存储策略。
type GetStoragePolicy struct {
	ginx.MethodGet
}

// Path 返回读取存储策略地址。
func (*GetStoragePolicy) Path() string { return "/policy" }

// Output 读取存储策略。
func (d *GetStoragePolicy) Output(ctx *gin.Context) (interface{}, error) {
	return global.StorageService.Policy(), nil
}
