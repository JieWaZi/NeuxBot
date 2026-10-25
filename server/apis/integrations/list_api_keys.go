package integrations

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// API Key 列表
type APIKeyList struct {
	// 密钥列表
	Resources []models.IntegrationKey `json:"resources"`
	// 总记录数
	TotalSize int64 `json:"totalSize"`
	// 当前页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// 获取 API Keys
type ListAPIKeys struct {
	ginx.MethodGet
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回密钥集合入口。
func (*ListAPIKeys) Path() string { return "/keys" }

// Validate 设置默认分页并拒绝无界枚举。
func (o *ListAPIKeys) Validate(*gin.Context) error {
	if o.PageNum == 0 {
		o.PageNum = 1
	}
	if o.PageSize == 0 {
		o.PageSize = 10
	}
	if o.PageNum < 1 || o.PageSize < 1 || o.PageSize > 100 || o.PageNum > 100000 {
		return status_error.IntegrationInvalid
	}
	return nil
}

// Output 返回摘要已隐藏的权威记录和实际总数。
func (o *ListAPIKeys) Output(ctx *gin.Context) (interface{}, error) {
	if err := available(); err != nil {
		return nil, err
	}
	if err := o.Validate(ctx); err != nil {
		return nil, err
	}
	var keys []models.IntegrationKey
	var total int64
	err := global.IntegrationService.WithAuthorizationWrite(ctx, func(tx context.Context) error {
		var err error
		keys, total, err = global.IntegrationKeyControllerManager.List(tx, o.PageSize, (o.PageNum-1)*o.PageSize)
		if err != nil {
			return err
		}
		// 只对当前有界管理页做对账；存储故障和不可用不能解释成资源不存在。
		missing := map[string]bool{}
		for i, key := range keys {
			removals := map[constants.IntegrationResourceKind][]string{}
			changed := false
			for _, grant := range key.Grants {
				for _, id := range grant.ResourceIDs {
					lookup := string(grant.ResourceKind) + ":" + id
					absent, checked := missing[lookup]
					if !checked {
						if grant.ResourceKind == constants.IntegrationResourceKindKnowledge {
							if global.KnowledgeEngine == nil {
								return status_error.KnowledgeUnsupported
							}
							_, err = global.KnowledgeEngine.GetLibrary(tx, id)
							if err != nil {
								err = bridge.MapError(err)
							}
							absent = errors.Is(err, status_error.KnowledgeNotFound)
						} else {
							if global.AutomaticTaskControllerManager == nil {
								return status_error.IntegrationInvalid
							}
							_, err = global.AutomaticTaskControllerManager.Get(tx, id)
							absent = errors.Is(err, status_error.AutomaticTaskNotFound)
						}
						if err != nil && !absent {
							return err
						}
						missing[lookup] = absent
					}
					if absent {
						changed = true
						removals[grant.ResourceKind] = append(removals[grant.ResourceKind], id)
					}
				}
			}
			if changed {
				keys[i], err = global.IntegrationKeyControllerManager.RemoveResourceGrants(tx, key.ID, key.Revision, removals)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return APIKeyList{Resources: keys, TotalSize: total, PageNum: o.PageNum, PageSize: o.PageSize}, nil
}
