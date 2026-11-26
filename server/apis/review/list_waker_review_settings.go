package review

import (
	"math"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴复盘设置列表
type ListWakerReviewSettings struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 伙伴复盘设置条目
type WakerReviewSettingsItem struct {
	// 伙伴标识
	WakerID string `json:"wakerId"`
	// 伙伴名称
	Name string `json:"name"`
	// 伙伴头像
	Avatar string `json:"avatar"`
	// 复盘偏好
	WakerReviewInput
}

// 伙伴复盘设置列表响应
type ListWakerReviewSettingsResponse struct {
	// 资源列表
	Resources []WakerReviewSettingsItem `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// Path 返回伙伴复盘设置列表路径。
func (*ListWakerReviewSettings) Path() string { return "/wakers/settings" }

// Validate 校验分页范围并补充默认值。
func (o *ListWakerReviewSettings) Validate(*gin.Context) error {
	if o.PageNum < 0 || o.PageSize < 0 || o.PageSize > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if o.PageNum == 0 {
		o.PageNum = 1
	}
	if o.PageSize == 0 {
		o.PageSize = 50
	}
	if o.PageNum-1 > math.MaxInt/o.PageSize {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 返回当前页伙伴的已保存偏好。
func (o *ListWakerReviewSettings) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	query := strings.TrimSpace(o.Query)
	wakers, err := global.WakerControllerManager.List(ctx, query, o.PageSize, (o.PageNum-1)*o.PageSize)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	total, err := global.WakerControllerManager.Count(ctx, query)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	ids := make([]string, 0, len(wakers))
	for _, waker := range wakers {
		ids = append(ids, waker.ID)
	}
	preferences, err := global.ReviewWorker.PartnerPreferences(ctx, ids)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]WakerReviewSettingsItem, 0, len(wakers))
	for _, waker := range wakers {
		preference := preferences[waker.ID]
		if preference.SkillMode == "" {
			preference.SkillMode = reviewstate.ModeOff
		}
		resources = append(resources, WakerReviewSettingsItem{
			WakerID: waker.ID,
			Name:    waker.Name,
			Avatar:  waker.Avatar,
			WakerReviewInput: WakerReviewInput{
				MemoryEnabled: preference.MemoryEnabled,
				SkillMode:     preference.SkillMode,
			},
		})
	}
	return ListWakerReviewSettingsResponse{Resources: resources, PageNum: o.PageNum, PageSize: o.PageSize, TotalSize: total}, nil
}
