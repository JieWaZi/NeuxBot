package channel

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

// submitMemberReport 持久接纳当前成员尝试的真实报告，并尽早投影到负责人 Chat。
func (s *service) submitMemberReport(
	ctx context.Context,
	thread models.ChannelThread,
	member models.ChannelThreadMember,
	capability channeltool.Scope,
	text string,
	readyForUser bool,
) (channeltool.TaskOutput, error) {
	report := strings.TrimSpace(text)
	if report == "" {
		return channelToolError(status_error.AgentInputInvalid, "text is required for report.")
	}
	if len([]rune(report)) > constants.ChannelMemberReportMaximumRunes {
		return channelToolError(status_error.AgentInputInvalid, "The report exceeds 4,000 characters. Submit a concise complete report.")
	}
	task, err := s.tasks.Get(ctx, capability.TaskID)
	if err != nil || task.ThreadID != thread.ID ||
		task.AssigneeWakerID != capability.CallerWakerID ||
		task.Status != constants.ChannelTaskStatusInProgress {
		if err != nil {
			logChannelToolFailure(capability, constants.ChannelTaskToolActionReport, capability.TaskID, err)
			return channelToolError(err, "The report capability does not belong to the current in-progress task.")
		}
		return channelToolError(status_error.ChannelTaskAttemptStale, "The report capability does not belong to the current in-progress task.")
	}
	attempt, err := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
	if err != nil || attempt.ID != capability.AttemptID ||
		attempt.ExecutionID != capability.ExecutionID ||
		attempt.AssigneeWakerID != capability.CallerWakerID ||
		attempt.ChatID != member.ChatID {
		if err != nil {
			logChannelToolFailure(capability, constants.ChannelTaskToolActionReport, capability.TaskID, err)
			return channelToolError(err, "The report capability does not belong to the current task attempt.")
		}
		return channelToolError(status_error.ChannelTaskAttemptStale, "The report capability does not belong to the current task attempt.")
	}
	existing := strings.TrimSpace(attempt.Result)
	if existing != "" && existing != report {
		return channelToolError(status_error.AgentCommandRequestConflict, "A work report was already submitted for this task attempt.")
	}
	if existing != "" && attempt.UserReady != readyForUser {
		return channelToolError(status_error.AgentCommandRequestConflict, "The submitted report cannot change its delivery status.")
	}
	if err := s.transaction(ctx, func(ctx context.Context) error {
		currentTask, err := s.tasks.Get(ctx, task.ID)
		if err != nil {
			return err
		}
		if currentTask.CurrentFence != task.CurrentFence || currentTask.Status != constants.ChannelTaskStatusInProgress {
			return status_error.ChannelTaskAttemptStale
		}
		current, err := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
		if err != nil {
			return err
		}
		if current.ID != attempt.ID || (current.Result != "" && (current.Result != report || current.UserReady != readyForUser)) {
			return status_error.AgentCommandRequestConflict
		}
		if err := s.tasks.UpdateAttemptFields(ctx, attempt.ID, map[string]interface{}{"result": report, "user_ready": readyForUser, "updated_at": time.Now().UnixMilli()}); err != nil {
			return err
		}
		return agentcommand.Commit(ctx, map[string]any{"reported": true, "taskId": task.ID, "attemptId": attempt.ID, "nextStep": constants.ChannelMemberReportFinalInstruction})
	}); err != nil {
		return channeltool.TaskOutput{}, err
	}

	if err := s.chats.AppendParticipantMessages(ctx, thread.LeadChatID, []chat.ParticipantMessageInput{{
		ID:            attempt.ID,
		Actor:         string(constants.ChannelMessageActorMemberReport),
		Content:       report,
		SourceWakerID: member.WakerID,
		SourceChatID:  member.ChatID,
	}}); err != nil && !isChatBusy(err) {
		// 报告已经持久化；终态提交会再次幂等投影，不能诱导成员改写已接纳报告。
		logChannelToolFailure(capability, constants.ChannelTaskToolActionReport, capability.TaskID, err)
	}
	return channeltool.TaskOutput{
		Reported: true,
		NextStep: constants.ChannelMemberReportFinalInstruction,
	}, nil
}
