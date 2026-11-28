package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func (s *Service) drainConclusions(ctx context.Context, after string) string {
	const batch = 16
	runs, err := s.runs.ListPendingConclusions(ctx, after, batch)
	if err != nil && ctx.Err() == nil {
		logx.Errorf("load pending workflow conclusions: error=%v", err)
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return ""
		}
		err := s.deliverConclusion(ctx, run.ID, false)
		if err != nil && ctx.Err() == nil && !util.HasStatusCode(err, status_error.WorkflowStateConflict) {
			logx.Warnf("publish workflow conclusion: run_id=%s error=%v", run.ID, err)
		}
	}
	if len(runs) == batch {
		return runs[len(runs)-1].ID
	}
	return ""
}

// RetryConclusion 补交已结算的整图结论，仍使用原运行和接纳时的地址。
func (s *Service) RetryConclusion(ctx context.Context, runID string) error {
	return s.deliverConclusion(ctx, runID, true)
}

func (s *Service) deliverConclusion(ctx context.Context, runID string, retry bool) error {
	_, err, _ := s.chatDeliveries.Do("conclusion:"+runID, func() (any, error) {
		run, err := s.runs.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if !run.ConclusionRequested || !workflowstate.TerminalRun(run.Status) {
			return nil, status_error.WorkflowStateConflict
		}
		if run.ConclusionFinishedAt != nil && (!retry || run.ConclusionDeliveryError == "") {
			return nil, nil
		}
		return nil, s.publishConclusion(ctx, run)
	})
	return err
}

func (s *Service) publishConclusion(ctx context.Context, run models.WorkflowRun) error {
	finish := func(ctx context.Context, err error) error {
		code := ""
		switch {
		case util.HasStatusCode(err, status_error.ChatNotFound), util.HasStatusCode(err, status_error.ChatMessageNotFound),
			util.HasStatusCode(err, status_error.ExecutionNotFound), util.HasStatusCode(err, status_error.WakerNotFound),
			util.HasStatusCode(err, status_error.ChannelNotFound), util.HasStatusCode(err, status_error.ChatReplyTargetUnavailable):
			code = util.ErrorCode(err, status_error.ChatNotFound.Key())
		case err != nil:
			return err
		}
		return s.transactions.Transaction(ctx, func(ctx context.Context) error {
			if err := s.runs.FinishConclusion(ctx, run.ID, code, time.Now().UnixMilli()); err != nil {
				return err
			}
			_, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: run.ID, EventID: util.New(), Type: constants.WorkflowRunChangedEvent, EntityID: run.ID,
				Payload: map[string]any{"runId": run.ID, "status": run.Status, "version": run.Version}})
			return err
		})
	}
	content := workflowConclusion(run)
	if run.ConclusionRecipient != nil && run.Status == constants.WorkflowRunStatusSucceeded {
		if run.Result == nil {
			return finish(ctx, nil)
		}
		if report, ok := run.Result.(string); ok {
			switch strings.TrimSpace(report) {
			case "", constants.AutomaticReportNoChanges:
				return finish(ctx, nil)
			}
			value := []rune(report)
			content = report
			if len(value) > 2000 {
				content = strings.TrimRight(string(value[:2000]), " \n") + "…"
			}
		}
	}
	if run.ConclusionMainID != "" || run.ConclusionRecipient != nil {
		if s.publishMainConclusion == nil {
			return status_error.WorkflowCapabilityUnsupported
		}
		return s.transactions.Transaction(ctx, func(ctx context.Context) error {
			return finish(ctx, s.publishMainConclusion(ctx, run, content))
		})
	}
	if s.chats == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	if run.ChatSummaryFinishedAt != nil {
		return finish(ctx, nil)
	}
	actual, err := s.chats.ReadExecution(ctx, run.SourceChatID, run.SourceExecutionID)
	if err != nil {
		return finish(ctx, err)
	}
	if actual.CompletedAt == nil {
		return status_error.WorkflowStateConflict
	}
	return finish(ctx, s.chats.PublishExecutionDelivery(ctx, chat.ExecutionDelivery{
		ChatID: run.SourceChatID, ExecutionID: run.SourceExecutionID, SourceID: run.ID,
		Summary: content, SummaryFallback: true,
	}))
}

func workflowConclusion(run models.WorkflowRun) string {
	name := run.SourceName
	if name == "" {
		name, _ = run.Context["workflowName"].(string)
	}
	status := "运行已完成"
	detail := run.Result
	switch run.Status {
	case constants.WorkflowRunStatusFailed:
		status = "运行失败"
		detail = run.Failure
	case constants.WorkflowRunStatusCancelled:
		status = "运行已停止"
		detail = run.Failure
	}
	content := fmt.Sprintf("工作流「%s」%s。", name, status)
	if detail == nil {
		return content
	}
	encoded, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return content
	}
	value := []rune(string(encoded))
	if len(value) > 2000 {
		return content + "\n\n~~~text\n" + strings.TrimRight(string(value[:2000]), " \n") + "…\n~~~"
	}
	return content + "\n\n~~~json\n" + string(encoded) + "\n~~~"
}
