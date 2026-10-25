package knowledge_connections

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 检查远端知识连接
type CheckConnection struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
}

// 连接检查结果
type ConnectionCheck struct {
	// 认证连接成功
	Connected bool `json:"connected"`
	// 可发现库数
	VisibleLibraries int64 `json:"visibleLibraries"`
	// 服务类型
	Backend string `json:"backend"`
}

func (o *CheckConnection) Path() string { return "/:connectionId/check" }
func (o *CheckConnection) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, "")
	if err != nil {
		return nil, err
	}
	var total int64
	if c.connection.Backend == "integration" {
		items, e := mountedLibraries(ctx, c)
		err = e
		total = int64(len(items))
	} else if c.official != nil {
		items, e := c.official.ListLibraries(ctx)
		err = e
		total = int64(len(items))
	} else {
		items, e := c.custom.ListLibraries(ctx, 0, 1)
		err = e
		if e == nil {
			total = items.Total
		}
	}
	if err != nil {
		return nil, remoteError(err)
	}
	return ConnectionCheck{Connected: true, VisibleLibraries: total, Backend: c.connection.Backend}, nil
}
