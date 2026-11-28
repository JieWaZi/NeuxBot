package schedule

import (
	"context"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
)

const (
	// ReportNoChanges 是已接纳的报告运行明确选择不汇报内容的完整正文。
	ReportNoChanges = constants.AutomaticReportNoChanges
)

const reportInstruction = "Give the user-facing report as the final answer, using the language of the task. If you completed the requested check and found no changes to report, return exactly " + ReportNoChanges + ". Do not use the marker when the check failed or you could not establish the result."

func validateReportConfiguration(config *models.AutomaticTaskConfiguration) error {
	switch {
	case config.WorkflowReportRecipient != nil:
		if config.ReportDestination != "" {
			return status_error.BadRequest
		}
	case config.ReportDestination == "":
		if config.ReportPolicy != "" {
			return status_error.BadRequest
		}
		return nil
	case config.ReportDestination == constants.AutomaticReportDestinationMainConversation ||
		config.ReportDestination == constants.AutomaticReportDestinationTopic:
		if config.Trigger == constants.AutomaticTaskTriggerAPI || config.WorkflowID != "" {
			return status_error.BadRequest
		}
	default:
		return status_error.BadRequest
	}
	switch config.ReportPolicy {
	case "":
		config.ReportPolicy = constants.AutomaticReportPolicyAlways
	case constants.AutomaticReportPolicyAlways, constants.AutomaticReportPolicyChangesOnly:
	default:
		return status_error.BadRequest
	}
	return nil
}

func (s *Service) reconcileReports(ctx context.Context) {
	if s.config.PublishReport == nil {
		return
	}
	const batchSize = 100
	for after := ""; ctx.Err() == nil; {
		rows, err := s.config.Tasks.PendingReports(ctx, after, batchSize)
		if err != nil {
			logx.Warnf("read pending automatic reports failed: error=%v", err)
			return
		}
		for _, run := range rows {
			after = run.ID
			if err := s.completeReport(ctx, run); err != nil {
				logx.Warnf("reconcile automatic report failed: run_id=%s error_code=%s", run.ID, util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()))
			}
		}
		if len(rows) < batchSize {
			return
		}
	}
}

func (s *Service) completeReport(ctx context.Context, run models.AutomaticRun) error {
	view, err := s.ReadRun(ctx, run)
	if err != nil || Unfinished(view.State) {
		return err
	}
	if s.config.ReportReady != nil && !s.config.ReportReady(run) {
		return nil
	}
	if run.ReportByAgent {
		fresh, err := s.config.Tasks.GetRun(ctx, run.ID)
		if err != nil || fresh.ReportState != constants.AutomaticReportStatePending {
			return err
		}
		if fresh.AgentReportKey != "" {
			return s.deliverAgentReport(ctx, fresh, fresh.AgentReportBody)
		}
		if view.State == constants.AutomaticRunStateFailed {
			body := "The automatic task failed. Open its run details to see what happened."
			if strings.ContainsFunc(run.Configuration.Instruction, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				body = "本次定时任务执行失败，可在运行详情查看原因。"
			}
			return s.deliverAgentReport(ctx, fresh, body)
		}
		state := constants.AutomaticReportStateUnavailable
		if view.State == constants.AutomaticRunStateCompleted && fresh.Configuration.ReportPolicy == constants.AutomaticReportPolicyChangesOnly {
			state = constants.AutomaticReportStateQuiet
		}
		return s.config.Tasks.FinishReport(ctx, fresh.ID, state, "")
	}
	if view.State != constants.AutomaticRunStateCompleted {
		return s.config.Tasks.FinishReport(ctx, run.ID, constants.AutomaticReportStateUnavailable, "")
	}
	if s.config.ReadReport == nil {
		return nil
	}
	body, err := s.config.ReadReport(ctx, run)
	if err != nil {
		return err
	}
	body = strings.TrimSpace(body)
	if body == "" || body == ReportNoChanges {
		if run.Configuration.ReportDestination == constants.AutomaticReportDestinationMainConversation ||
			run.Configuration.ReportPolicy == constants.AutomaticReportPolicyChangesOnly {
			return s.config.Tasks.FinishReport(ctx, run.ID, constants.AutomaticReportStateQuiet, "")
		}
		// 只有明确无变化的回执可以说明已检查。无正文的成功运行保留各自含义。
		body = noReportBody(run.Configuration.Instruction, body == ReportNoChanges)
	}
	href, err := s.config.PublishReport(ctx, run, body)
	if err != nil {
		return err
	}
	return s.config.Tasks.FinishReport(ctx, run.ID, constants.AutomaticReportStateDelivered, href)
}

// AgentReportBinding 只给本次自动运行的原伙伴或团队负责人签发发言权限。
func (s *Service) AgentReportBinding(ctx context.Context, source models.Chat, executionID, runID string) (capability.Binding, error) {
	run, err := s.config.Tasks.GetRun(ctx, runID)
	if util.HasStatusCode(err, status_error.AutomaticRunNotFound) {
		return capability.Binding{}, nil
	}
	if err != nil || !run.ReportByAgent || run.ReportState != constants.AutomaticReportStatePending || s.config.PublishReport == nil {
		return capability.Binding{}, err
	}
	if run.Configuration.ChannelID == "" {
		if source.ID != candidateChatID(run) || source.WakerID != run.WakerID || executionID != run.ExecutionID {
			return capability.Binding{}, nil
		}
	} else {
		if s.config.Threads == nil || (run.ChatID != "" && run.ChatID != source.ID) {
			return capability.Binding{}, nil
		}
		thread, found, err := s.config.Threads.FindByChatID(ctx, source.ID)
		if err != nil || !found || thread.LeadChatID != source.ID || thread.LeadWakerID != source.WakerID ||
			thread.AutomaticRunID != runID || thread.ChannelID != run.Configuration.ChannelID {
			return capability.Binding{}, err
		}
	}
	return capability.Binding{
		Operation: constants.ConversationMessagePostOperation,
		Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			body, _ := request.Params["content"].(string)
			body = strings.TrimSpace(body)
			if quoted, _ := request.Params["quoteReply"].(bool); quoted || body == "" || !capability.ValidRequestID(request.RequestID) {
				return capability.Result{}, status_error.AgentInputInvalid
			}
			reserved, err := s.config.Tasks.ReserveAgentReport(ctx, runID, request.RequestID, body)
			if err != nil {
				return capability.Result{}, err
			}
			stored, err := s.config.Tasks.GetRun(ctx, runID)
			if err != nil {
				return capability.Result{}, err
			}
			if stored.AgentReportKey != request.RequestID || stored.AgentReportBody != body {
				return capability.Result{}, status_error.AgentCommandRequestConflict
			}
			if stored.ReportState == constants.AutomaticReportStatePending {
				if stored.ChatID == "" {
					// 首次原生回合可能在运行接纳投影之前调用工具。
					stored.ChatID, stored.WakerID = source.ID, source.WakerID
				}
				if err := s.deliverAgentReport(ctx, stored, body); err != nil {
					return capability.Result{}, err
				}
			} else if stored.ReportState != constants.AutomaticReportStateDelivered {
				return capability.Result{}, status_error.AgentContextUnauthorized
			}
			return agentReportReceipt(stored, !reserved), nil
		},
		Receipt: func(ctx context.Context, key string) (capability.Result, error) {
			if !capability.ValidRequestID(key) {
				return capability.Result{}, status_error.AgentReceiptNotFound
			}
			stored, err := s.config.Tasks.GetRun(ctx, runID)
			if err != nil {
				return capability.Result{}, err
			}
			if stored.AgentReportKey != key || stored.ReportState != constants.AutomaticReportStateDelivered {
				return capability.Result{}, status_error.AgentReceiptNotFound
			}
			return agentReportReceipt(stored, true), nil
		},
	}, nil
}

func (s *Service) deliverAgentReport(ctx context.Context, run models.AutomaticRun, body string) error {
	href, err := s.config.PublishReport(ctx, run, body)
	if err != nil {
		return err
	}
	if href == "" {
		return status_error.AutomaticTaskServiceInvalid
	}
	return s.config.Tasks.FinishReport(ctx, run.ID, constants.AutomaticReportStateDelivered, href)
}

func agentReportReceipt(run models.AutomaticRun, replayed bool) capability.Result {
	id := AutomaticReportMessageID(run)
	return capability.Result{CommandID: id, Replayed: replayed, Data: map[string]any{"messageId": id}}
}

// AutomaticReportMessageID 返回与实际主会话或专属话题相同的持久消息身份。
func AutomaticReportMessageID(run models.AutomaticRun) string {
	if run.Configuration.ReportDestination == constants.AutomaticReportDestinationTopic {
		return (requestidempotency.Identity{ID: run.ID}).ResourceID("automatic-report", "message")
	}
	return mainconversation.AutomaticReportMessageID(run.ID)
}

func noReportBody(instruction string, verifiedNoChanges bool) string {
	chinese := strings.ContainsFunc(instruction, func(r rune) bool { return unicode.Is(unicode.Han, r) })
	switch {
	case chinese && verifiedNoChanges:
		return "本次没有需要汇报的变化。"
	case chinese:
		return "本次运行已完成，未生成报告正文。"
	case verifiedNoChanges:
		return "No changes to report."
	default:
		return "The run completed without a report."
	}
}
