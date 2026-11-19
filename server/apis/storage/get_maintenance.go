package storage

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetStorageMaintenance 读取清理进度。
type GetStorageMaintenance struct {
	ginx.MethodGet
}

// Path 返回读取清理进度地址。
func (*GetStorageMaintenance) Path() string { return "/maintenance" }

// Output 读取清理进度。
func (d *GetStorageMaintenance) Output(ctx *gin.Context) (interface{}, error) {
	return global.StorageService.Maintenance(), nil
}
