package work_board

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListWorkBoard 接收下方列表的范围及 seek 游标。
type ListWorkBoard struct {
	// MethodGet 声明只读 HTTP 方法。
	ginx.MethodGet
	// Mode 是 all、attention 或 results。
	Mode string `in:"query" name:"mode" validate:"omitempty,oneof=all attention results"`
	// ExecutorID 是伙伴、团队或流程定义的原标识。
	ExecutorID string `in:"query" name:"executorId" validate:"omitempty,len=32"`
	// Source 是原入口来源。
	Source string `in:"query" name:"source" validate:"omitempty,oneof=chat team workflow automatic im"`
	// Query 仅匹配原工作标题。
	Query string `in:"query" name:"query" validate:"max=100"`
	// Activity 过滤当前执行状态。
	Activity constants.ChannelActivityState `in:"query" name:"activity"`
	// 状态多选
	States string `in:"query" name:"states" validate:"max=100"`
	// From 是全部工作最近活动下界，零默认最近三十天。
	From int64 `in:"query" name:"from" validate:"min=0"`
	// To 是全部工作最近活动上界。
	To int64 `in:"query" name:"to" validate:"min=0"`
	// Cursor 是不透明的时间、种类、身份锚点。
	Cursor string `in:"query" name:"cursor" validate:"max=1024"`
	// 页码跳转
	Page int `in:"query" name:"page" validate:"min=0,max=100000000"`
	// Limit 默认五十且最多一百行。
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// WorkBoardPage 返回当前筛选的准确总数及游标页。
type WorkBoardPage struct {
	// 筛选总数
	TotalSize int64 `json:"totalSize"`
	// Resources 是当前工作或结果页。
	Resources []models.WorkBoardEntry `json:"resources"`
	// Attention 是当前需要操作页，按等待最久优先。
	Attention []models.WorkBoardAttention `json:"attention"`
	// HasMore 表示查询窗口之外仍存在候选。
	HasMore bool `json:"hasMore"`
	// NextCursor 是当前页最后一项的稳定身份。
	NextCursor string `json:"nextCursor,omitempty"`
}

// Path 返回看板列表路径。
func (*ListWorkBoard) Path() string { return "" }

// Validate 拒绝未知状态、倒置时间和非法游标。
func (d *ListWorkBoard) Validate(*gin.Context) error {
	for _, raw := range strings.Split(d.States, ",") {
		if raw == "" {
			continue
		}
		if d.Mode == "attention" {
			switch constants.NotificationKind(raw) {
			case constants.NotificationKindApproval, constants.NotificationKindInput, constants.NotificationKindReview:
				continue
			default:
				return status_error.BadRequest
			}
		}
		var state constants.WorkBoardState
		if state.UnmarshalText([]byte(raw)) != nil {
			return status_error.BadRequest
		}
	}
	if d.Activity != "" && d.Activity != constants.ChannelActivityStateIdle && d.Activity != constants.ChannelActivityStateRunning && d.Activity != constants.ChannelActivityStateWaiting {
		return status_error.BadRequest
	}
	if d.To > 0 && d.From > d.To {
		return status_error.BadRequest
	}
	_, err := decodeCursor(d.Cursor)
	return err
}

func decodeCursor(raw string) (*controllers.WorkBoardCursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, status_error.BadRequest
	}
	var v controllers.WorkBoardCursor
	if json.Unmarshal(data, &v) != nil || v.At <= 0 || v.ID == "" || len(v.ID) > 200 || !validOwner(v.Kind) {
		return nil, status_error.BadRequest
	}
	return &v, nil
}

func encodeCursor(v controllers.WorkBoardCursor) string {
	data, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(data)
}

func queryContext(ctx *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx.Request.Context(), 500*time.Millisecond)
}

// Output 将模式交给同一个只读仓储，不从前端拼接各模块全集。
func (d *ListWorkBoard) Output(ctx *gin.Context) (interface{}, error) {
	cursor, err := decodeCursor(d.Cursor)
	if err != nil {
		return nil, err
	}
	limit := d.Limit
	if limit == 0 {
		limit = 50
	}
	query := controllers.WorkBoardQuery{ExecutorID: d.ExecutorID, Source: d.Source, Query: d.Query, Activity: d.Activity, States: strings.FieldsFunc(d.States, func(r rune) bool { return r == ',' }), From: d.From, To: d.To, Cursor: cursor, Limit: limit, Offset: max(d.Page-1, 0) * limit}
	if cursor != nil {
		query.Offset = 0
	}
	if query.From == 0 {
		query.From = time.Now().AddDate(0, 0, -30).UnixMilli()
	}
	request, cancel := queryContext(ctx)
	defer cancel()
	var value WorkBoardPage
	err = global.TransactionControllerManager.Transaction(request, func(txctx context.Context) error {
		var readErr error
		value, readErr = d.readPage(txctx, query, limit, cursor)
		return readErr
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}

// readPage 在用例的同一事务内读取准确总量与当前页。
func (d *ListWorkBoard) readPage(request context.Context, query controllers.WorkBoardQuery, limit int, cursor *controllers.WorkBoardCursor) (WorkBoardPage, error) {
	var err error
	page := WorkBoardPage{Resources: []models.WorkBoardEntry{}, Attention: []models.WorkBoardAttention{}}
	if d.Mode == "attention" {
		page.Attention, page.TotalSize, err = global.WorkBoardControllerManager.Attention(request, query)
		if err != nil {
			return WorkBoardPage{}, ginx.WithStack(err)
		}
		page.HasMore = len(page.Attention) > limit
		if page.HasMore {
			page.Attention = page.Attention[:limit]
			v := page.Attention[limit-1]
			page.NextCursor = encodeCursor(controllers.WorkBoardCursor{At: v.CreatedAt, Kind: v.OwnerKind, ID: v.ID})
		}
	} else {
		if d.Mode == "results" {
			page.TotalSize, err = global.WorkBoardControllerManager.ResultCount(request, query)
			if err != nil {
				return WorkBoardPage{}, ginx.WithStack(err)
			}
			if page.TotalSize == 0 {
				return page, nil
			}
			if cursor == nil && d.Page > 1 {
				page.Resources, err = global.WorkBoardControllerManager.Page(request, query, true)
			} else {
				page.Resources, err = global.WorkBoardControllerManager.Results(request, query)
			}
		} else {
			total, countErr := global.WorkBoardControllerManager.Count(request, query)
			if countErr != nil {
				return WorkBoardPage{}, ginx.WithStack(countErr)
			}
			page.TotalSize = total
			if cursor == nil && d.Page > 1 {
				page.Resources, err = global.WorkBoardControllerManager.Page(request, query, false)
			} else {
				page.Resources, err = global.WorkBoardControllerManager.List(request, query)
			}
		}
		if err != nil {
			return WorkBoardPage{}, ginx.WithStack(err)
		}
		page.HasMore = len(page.Resources) > limit
		if page.HasMore {
			page.Resources = page.Resources[:limit]
			v := page.Resources[limit-1]
			page.NextCursor = encodeCursor(controllers.WorkBoardCursor{At: v.UpdatedAt, Kind: v.OwnerKind, ID: v.OwnerID})
		}
	}
	if page.Resources == nil {
		page.Resources = []models.WorkBoardEntry{}
	}
	if page.Attention == nil {
		page.Attention = []models.WorkBoardAttention{}
	}
	return page, nil
}
