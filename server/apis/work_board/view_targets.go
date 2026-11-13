package work_board

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetWorkBoardViewTargets 固定当前筛选下待查看的原目标。
type GetWorkBoardViewTargets struct {
	// MethodGet 声明只读请求。
	ginx.MethodGet
	// ExecutorID 是原执行对象筛选。
	ExecutorID string `in:"query" name:"executorId" validate:"omitempty,len=32"`
	// Source 是工作入口来源。
	Source string `in:"query" name:"source" validate:"omitempty,oneof=chat team workflow automatic im"`
	// Query 按原工作标题筛选。
	Query string `in:"query" name:"query" validate:"max=100"`
	// Activity 筛选原工作当前活动状态。
	Activity constants.ChannelActivityState `in:"query" name:"activity"`
	// States 是逗号分隔的可选工作状态。
	States string `in:"query" name:"states" validate:"max=100"`
}

// WorkBoardViewTargets 返回本次操作固定的有界目标。
type WorkBoardViewTargets struct {
	// Targets 保存准确的原工作及阅读边界。
	Targets []models.WorkViewTarget `json:"targets"`
	// TargetCount 是本次固定目标数量。
	TargetCount int `json:"targetCount"`
	// Epoch 是当前权威 Core 的生命周期标识。
	Epoch string `json:"epoch"`
}

func (*GetWorkBoardViewTargets) Path() string { return "/view-targets" }
func (d *GetWorkBoardViewTargets) Validate(ctx *gin.Context) error {
	return (&ListWorkBoard{Mode: "awaiting_view", Activity: d.Activity, States: d.States}).Validate(ctx)
}
func (d *GetWorkBoardViewTargets) Output(ctx *gin.Context) (interface{}, error) {
	const maximumTargets = 10000
	reader := constants.LocalReaderID
	filter := controllers.WorkBoardQuery{ExecutorID: d.ExecutorID, Source: d.Source, Query: d.Query, Activity: d.Activity, States: strings.FieldsFunc(d.States, func(r rune) bool { return r == ',' }), Limit: maximumTargets}
	if isolated := global.WorkspaceAttention.Isolated; isolated != nil {
		filter.IsolatedChats = isolated()
	}
	board := global.WorkBoardControllerManager
	var response WorkBoardViewTargets
	err := global.TransactionControllerManager.Transaction(ctx.Request.Context(), func(tx context.Context) error {
		missing, err := board.MissingReading(tx, reader, filter)
		if err != nil {
			return err
		}
		health, err := boardCompleteness(tx, filter, missing)
		if err != nil {
			return err
		}
		if !health.Complete {
			return status_error.ReadingProjectionIncomplete
		}
		rows, err := board.AwaitingView(tx, reader, filter)
		if err != nil {
			return err
		}
		if len(rows) > maximumTargets {
			return status_error.ReadingTargetLimitExceeded
		}
		readings, err := board.AttachReadings(tx, reader, rows, filter.IsolatedChats)
		if err != nil {
			return err
		}
		targets := make([]models.WorkViewTarget, 0, len(rows))
		for _, row := range rows {
			target := models.WorkViewTarget{Work: models.WorkRef{Kind: row.OwnerKind, ID: row.OwnerID}}
			if row.ChatID != "" {
				reading := readings[row.ChatID]
				if reading.LatestReadableMessageID == "" {
					return status_error.ReadingProjectionIncomplete
				}
				target.ChatID = row.ChatID
				target.ThroughMessageID = reading.LatestReadableMessageID
				target.ObservedUnreadGeneration = reading.UnreadGeneration
			} else {
				target.OwnerKind = constants.RunOwnerKind(row.OwnerKind)
				target.OwnerID = row.OwnerID
			}
			targets = append(targets, target)
		}
		health, err = boardCompleteness(tx, filter, false)
		if err != nil {
			return err
		}
		if !health.Complete {
			return status_error.ReadingProjectionIncomplete
		}
		response = WorkBoardViewTargets{Targets: targets, TargetCount: len(targets), Epoch: health.Epoch}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if len(encoded) > 4<<20 {
		return nil, ginx.WithStack(status_error.ReadingTargetLimitExceeded)
	}
	return response, nil
}
