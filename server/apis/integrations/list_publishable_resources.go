package integrations

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 可发布资源
type PublishableResource struct {
	// 资源标识
	ID string `json:"id"`
	// 资源名称
	Name string `json:"name"`
}

// 可发布资源列表
type PublishableResourceList struct {
	// 资源列表
	Resources []PublishableResource `json:"resources"`
	// 总记录数
	TotalSize int64 `json:"totalSize"`
}

// 获取可发布资源
type ListPublishableResources struct {
	ginx.MethodGet
	// 资源类型
	Kind constants.IntegrationResourceKind `in:"query" name:"kind" validate:"required"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回本机可授权目录，不使用跨来源知识列表。
func (*ListPublishableResources) Path() string { return "/resources" }

// Validate 限定明确分页。
func (o *ListPublishableResources) Validate(ctx *gin.Context) error {
	p := ListAPIKeys{PageNum: o.PageNum, PageSize: o.PageSize}
	if err := p.Validate(ctx); err != nil {
		return err
	}
	o.PageNum, o.PageSize = p.PageNum, p.PageSize
	return nil
}

// Output 仅列出本机权威知识库或实际可用 API 类型任务。
func (o *ListPublishableResources) Output(ctx *gin.Context) (interface{}, error) {
	if err := o.Validate(ctx); err != nil {
		return nil, err
	}
	result := PublishableResourceList{Resources: []PublishableResource{}}
	if o.Kind == constants.IntegrationResourceKindKnowledge {
		if global.KnowledgeEngine == nil {
			return nil, status_error.KnowledgeUnsupported
		}
		libraries, total, err := global.KnowledgeEngine.ListLibraries(ctx, (o.PageNum-1)*o.PageSize, o.PageSize)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		result.TotalSize = total
		for _, library := range libraries {
			result.Resources = append(result.Resources, PublishableResource{ID: library.ID, Name: library.Name})
		}
	} else {
		if global.AutomaticTaskControllerManager == nil {
			return nil, status_error.IntegrationInvalid
		}
		enabled := true
		tasks, total, err := global.AutomaticTaskControllerManager.List(ctx, controllers.AutomaticTaskFilter{Trigger: constants.AutomaticTaskTriggerAPI, Enabled: &enabled, Limit: o.PageSize, Offset: (o.PageNum - 1) * o.PageSize})
		if err != nil {
			return nil, err
		}
		result.TotalSize = total
		for _, task := range tasks {
			result.Resources = append(result.Resources, PublishableResource{ID: task.ID, Name: task.Name})
		}
	}
	return result, nil
}
