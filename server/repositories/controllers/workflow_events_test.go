package controllers

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowLogEventsFilterBeforePagination(t *testing.T) {
	db := newTestDB(t)
	controller := NewWorkflowController(db)
	ctx := context.Background()
	events := make([]models.WorkflowEvent, 0, 505)
	for sequence := int64(1); sequence <= 501; sequence++ {
		events = append(events, models.WorkflowEvent{
			StreamID: "run", Sequence: sequence, EventID: util.New(),
			Type: constants.WorkflowRunChangedEvent, Payload: map[string]any{},
		})
	}
	for index, kind := range []string{constants.WorkflowLogEvent, constants.WorkflowWorkerSkipRequestedEvent, constants.WorkflowLogEvent} {
		events = append(events, models.WorkflowEvent{
			StreamID: "run", Sequence: int64(502 + index), EventID: util.New(),
			Type: kind, Payload: map[string]any{"value": "真实日志"},
		})
	}
	events = append(events, models.WorkflowEvent{
		StreamID: "other-run", Sequence: 1, EventID: util.New(),
		Type: constants.WorkflowLogEvent, Payload: map[string]any{},
	})
	if err := db.CreateInBatches(events, 100).Error; err != nil {
		t.Fatal(err)
	}
	logs, err := controller.ListLogEvents(ctx, "run", 0, 2)
	if err != nil || len(logs) != 2 || logs[0].Sequence != 502 || logs[1].Sequence != 503 {
		t.Fatalf("日志过滤或分页范围错误: %+v %v", logs, err)
	}
	tail, err := controller.ListLogEvents(ctx, "run", logs[1].Sequence, 2)
	if err != nil || len(tail) != 1 || tail[0].Sequence != 504 {
		t.Fatalf("日志游标未保持原事件序号: %+v %v", tail, err)
	}
	all, err := controller.ListEvents(ctx, "run", 0, 2)
	if err != nil || len(all) != 2 || all[0].Sequence != 1 || all[1].Sequence != 2 {
		t.Fatalf("日志查询改变了原事件重放: %+v %v", all, err)
	}
}
