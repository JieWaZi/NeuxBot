package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

const chatSummaryTimeout = time.Minute

// RetryChatDelivery 只对原运行补交附件或补写总结，不产生新 Run。
func (s *Service) RetryChatDelivery(ctx context.Context, chatID, runID string, summary bool) error {
	return s.deliverChatRun(ctx, chatID, runID, summary, true)
}

func (s *Service) deliverChatRun(ctx context.Context, chatID, runID string, summary, requested bool) error {
	if s.chats == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	key := "files:" + runID
	if summary {
		key = "summary:" + runID
	}
	// 归属在加入共享补交前先检查，外部用户输入不能等待另一个 Chat 的命令结果。
	run, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.SourceChatID != chatID || run.SourceWorkbench || requested && run.HistoryDeletedAt != nil {
		return status_error.WorkflowNotFound
	}
	if !workflowstate.TerminalRun(run.Status) {
		return status_error.WorkflowStateConflict
	}
	_, err, _ = s.chatDeliveries.Do(key, func() (any, error) {
		current, err := s.runs.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if requested && current.HistoryDeletedAt != nil {
			return nil, status_error.WorkflowNotFound
		}
		if summary {
			if current.ChatSummaryFinishedAt != nil {
				return nil, nil
			}
			return nil, s.deliverChatSummary(ctx, current)
		}
		if current.ChatDeliveryFinishedAt != nil && (current.ChatDeliveryError == "" || !requested) {
			return nil, nil
		}
		return nil, s.deliverChatArtifacts(ctx, current)
	})
	return err
}

func (s *Service) deliverChatArtifacts(ctx context.Context, run models.WorkflowRun) error {
	delivery := chat.ExecutionDelivery{ChatID: run.SourceChatID, ExecutionID: run.SourceExecutionID, SourceID: run.ID}
	var updatedFiles map[string]models.WorkflowOutputFile
	var err error
	partial := run.Status != constants.WorkflowRunStatusSucceeded
	for _, field := range slices.Sorted(maps.Keys(run.OutputFiles)) {
		file := run.OutputFiles[field]
		if file.Partial != partial || file.Error != "" {
			continue
		}
		fileID := requestidempotency.Identity{ID: run.ID}.ResourceID("workflow-chat-output", outputFileID(field), run.SourceExecutionID)
		delivery.Files = append(delivery.Files, chat.ExecutionDeliveryFile{
			ID: fileID, Name: file.Name, Partial: file.Partial,
			Open: func() (io.ReadCloser, error) { return s.openOutputReceipt(run, file) },
			HandleSourceError: func(err error) bool {
				if !errors.Is(err, status_error.WorkflowFileNotFound) && !errors.Is(err, status_error.WorkflowFileInvalid) {
					return false
				}
				file.Error = util.ErrorCode(err, status_error.WorkflowFileInvalid.Key())
				run.OutputFiles[field] = file
				updatedFiles = run.OutputFiles
				return true
			},
		})
	}
	if len(delivery.Files) > 0 {
		err = s.chats.PublishExecutionDelivery(ctx, delivery)
	}
	if errors.Is(err, status_error.ChatHasActiveExecution) {
		return err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	code := ""
	if err != nil {
		code = util.ErrorCode(err, status_error.MessageAttachmentStorageError.Key())
	}
	if finishErr := s.runs.FinishChatRunFiles(ctx, run.SourceChatID, run.ID, code, updatedFiles, time.Now().UnixMilli()); finishErr != nil {
		return finishErr
	}
	return err
}

// ChatSnapshotError 返回本次成果中已确认无法取得的原始快照错误。
func ChatSnapshotError(run models.WorkflowRun) string {
	if !workflowstate.TerminalRun(run.Status) {
		return ""
	}
	partial := run.Status != constants.WorkflowRunStatusSucceeded
	for _, field := range slices.Sorted(maps.Keys(run.OutputFiles)) {
		if file := run.OutputFiles[field]; file.Partial == partial && file.Error != "" {
			return file.Error
		}
	}
	return ""
}

func (s *Service) deliverChatSummary(ctx context.Context, run models.WorkflowRun) error {
	actual, err := s.chats.ReadExecution(ctx, run.SourceChatID, run.SourceExecutionID)
	if err != nil {
		return err
	}
	if actual.CompletedAt == nil {
		return status_error.WorkflowStateConflict
	}
	if err := s.summarizeChatRun(ctx, run, actual); err != nil {
		return err
	}
	return s.runs.FinishChatRunSummary(ctx, run.SourceChatID, run.ID, time.Now().UnixMilli())
}

// deliveredChatPartialResults 从附件结算及各文件收据读取已交付的部分成果。
func deliveredChatPartialResults(run models.WorkflowRun) []map[string]string {
	results := []map[string]string{}
	if run.ChatDeliveryFinishedAt == nil || run.ChatDeliveryError != "" {
		return results
	}
	for _, field := range slices.Sorted(maps.Keys(run.OutputFiles)) {
		file := run.OutputFiles[field]
		if file.Partial && file.Error == "" {
			results = append(results, map[string]string{"field": field, "name": file.Name})
		}
	}
	return results
}

func (s *Service) summarizeChatRun(ctx context.Context, run models.WorkflowRun, actual models.Execution) error {
	records, err := s.chats.ReadExecutionRecords(run.SourceChatID, run.SourceExecutionID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.ExecutionID != run.SourceExecutionID || record.Type != constants.ChatEventTypeExecutionDelivery {
			continue
		}
		var delivery eventlog.ExecutionDeliveryData
		if json.Unmarshal(record.Data, &delivery) != nil {
			return status_error.ChatLogCorrupted
		}
		if delivery.SourceID == run.ID && delivery.Summary != "" && !delivery.SummaryFallback {
			return nil
		}
	}
	messages, err := s.chats.ReadExecutionMessages(ctx, run.SourceChatID, run.SourceExecutionID)
	if err != nil {
		return err
	}
	input := ""
	for _, message := range messages {
		if message.ID == run.SourceMessageID {
			input = message.Content
			break
		}
	}
	facts := map[string]any{"status": run.Status, "result": run.Result, "failure": run.Failure}
	if run.Status != constants.WorkflowRunStatusSucceeded {
		facts["partialResults"] = deliveredChatPartialResults(run)
	}
	prompt := maps.Clone(facts)
	prompt["userInput"], prompt["workflow"] = input, run.SourceName
	evidence, err := json.Marshal(prompt)
	if err != nil {
		return status_error.WorkflowSchemaInvalid
	}
	var text string
	if s.summarizeOutput == nil {
		err = status_error.WorkflowCapabilityUnsupported
	} else {
		generation, cancel := context.WithTimeout(ctx, chatSummaryTimeout)
		text, err = s.summarizeOutput(generation, models.WorkflowExecutionDefaults{RuntimeID: actual.RuntimeID, AccountID: actual.AccountID, Model: actual.Model, Reasoning: actual.Reasoning, Fast: actual.Fast}, string(evidence))
		if errors.Is(generation.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			err = status_error.OperationTimeout
		}
		cancel()
	}
	if err == nil && strings.TrimSpace(text) == "" {
		err = status_error.WorkflowSchemaInvalid
	}
	if ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	delivery := chat.ExecutionDelivery{ChatID: run.SourceChatID, ExecutionID: run.SourceExecutionID, SourceID: run.ID, Summary: text}
	if err != nil {
		// 原智能体无法做受限补写时仍交付运行已提交的事实，保留独立的补写错误与重试入口。
		confirmed, _ := json.MarshalIndent(facts, "", "  ")
		delivery.Summary = "~~~json\n" + string(confirmed) + "\n~~~"
		delivery.SummaryFallback = true
	}
	if publishErr := s.chats.PublishExecutionDelivery(ctx, delivery); publishErr != nil {
		return publishErr
	}
	return err
}

func (s *Service) drainChatDeliveries(ctx context.Context, after string) string {
	queued, err := s.runs.ListPendingChatDeliveries(ctx, after, 16)
	if err != nil && ctx.Err() == nil {
		logx.Errorf("load pending workflow chat attachments failed: error=%v", err)
		return ""
	}
	for _, run := range queued {
		if ctx.Err() != nil {
			return ""
		}
		if err := s.deliverChatRun(ctx, run.SourceChatID, run.ID, false, false); err != nil && !errors.Is(err, status_error.ChatHasActiveExecution) && ctx.Err() == nil {
			logx.Warnf("deliver workflow chat attachments failed: run_id=%s error=%v", run.ID, err)
		}
	}
	if len(queued) == 16 {
		return queued[len(queued)-1].ID
	}
	return ""
}

func (s *Service) runChatDeliveryLoop(lifecycle *runnerLifecycle) {
	defer lifecycle.workers.Done()
	// 唤醒处理正常终态，低频扫描只用于恢复漏送通知和临时会话占用。
	retry := time.NewTicker(3 * time.Second)
	defer retry.Stop()
	after, afterConclusion := "", ""
	for {
		after = s.drainChatDeliveries(lifecycle.ctx, after)
		afterConclusion = s.drainConclusions(lifecycle.ctx, afterConclusion)
		select {
		case <-lifecycle.ctx.Done():
			return
		case <-lifecycle.deliveryWake:
		case <-retry.C:
		}
	}
}

func (s *Service) notifyTerminalChat(l *runnerLifecycle, run models.WorkflowRun) {
	if !run.ConclusionRequested && (run.SourceChatID == "" || run.SourceWorkbench) {
		return
	}
	select {
	case l.deliveryWake <- struct{}{}:
	default:
	}
}
