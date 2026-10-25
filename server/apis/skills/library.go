package skills

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
)

// ListLibrarySkills 分页查询全局资源。
type ListLibrarySkills struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回全局资源列表入口。
func (*ListLibrarySkills) Path() string { return "" }

// ListLibrarySkillsResponse 是准确分页结果。
type ListLibrarySkillsResponse struct {
	// 资源列表
	Resources []WakerSkillResponse `json:"resources"`
	// 准确总数
	TotalSize int64 `json:"totalSize"`
	// 当前页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// Output 使用单表分页和有界关联补齐。
func (o *ListLibrarySkills) Output(ctx *gin.Context) (interface{}, error) {
	page := max(o.PageNum, 1)
	size := o.PageSize
	if size < 1 || size > 100 {
		size = 24
	}
	var items []models.SkillWithState
	var total int64
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		items, total, err = global.SkillControllerManager.ListLibrary(txctx, strings.TrimSpace(o.Query), (page-1)*size, size)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources, err := wakerSkillResponses(ctx, items)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListLibrarySkillsResponse{Resources: resources, TotalSize: total, PageNum: page, PageSize: size}, nil
}

// InstallLibrarySkillRequest 描述明确选择的安装伙伴。
type InstallLibrarySkillRequest struct {
	// 所选伙伴
	WakerIDs []string `json:"wakerIds" validate:"required,min=1,max=100,dive,required"`
}

// InstallLibrarySkill 只建立全局资源安装关系。
type InstallLibrarySkill struct {
	ginx.MethodPost
	// 资源标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body InstallLibrarySkillRequest `in:"body"`
}

// Path 返回明确安装操作入口。
func (*InstallLibrarySkill) Path() string { return "/:skillId/install" }

// Output 不复制共享内容或设置，重复安装保留原启停状态。
func (o *InstallLibrarySkill) Output(ctx *gin.Context) (interface{}, error) {
	if len(o.Body.WakerIDs) == 0 || len(o.Body.WakerIDs) > 100 {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		resource, err := global.SkillControllerManager.GetPackage(txctx, o.SkillID)
		if err != nil {
			return err
		}
		if resource.Scope != constants.SkillScopePublic {
			return status_error.BadRequest
		}
		for _, id := range o.Body.WakerIDs {
			if _, err := global.WakerControllerManager.Get(txctx, id); err != nil {
				return err
			}
			if err := global.SkillControllerManager.EnsureWakerSkill(txctx, models.WakerSkill{ID: util.New(), WakerID: id, SkillPackageID: o.SkillID, Enabled: true}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, o.SkillID)
	return ginx.CommonSuccessResponse(), nil
}
