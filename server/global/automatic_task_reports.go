package global

import (
	"context"
	"net/url"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
)

func automaticTaskRuleBindings(ctx context.Context, owner models.Chat, executionID string, config execution.WorkOptions, entry string) []capability.Binding {
	if ScheduleService == nil || DB == nil {
		return nil
	}
	scope := schedule.RuleScope{
		WakerID: owner.WakerID, RuntimeID: config.RuntimeID, AccountID: config.AccountID, Model: config.Model,
		Reasoning: config.Reasoning, ProjectID: owner.ProjectID, Timezone: automaticRuleTimezone(),
		PermissionLevel: config.PermissionLevel, WorkspaceReadOnly: config.WorkspaceReadOnly,
	}
	var main models.MainConversation
	if entry == execution.MainConversationEntryPoint || entry == execution.MainConversationSourceEntryPoint {
		work, err := MainConversationControllerManager.Work(ctx, owner.ID)
		if err != nil || work.WakerID != owner.WakerID {
			return nil
		}
		main, err = MainConversationControllerManager.Get(ctx, work.ConversationID)
		if err != nil || MainConversationService.ValidateRouterActor(ctx, main, owner.WakerID) != nil {
			return nil
		}
		if main.SubjectType == "team" {
			scope.ChannelID = main.SubjectID
		}
	}
	bindings := ScheduleService.ConversationRuleBindings(controllers.NewAgentCommandController(DB), owner.ID+":"+executionID, func(ctx context.Context) (schedule.RuleScope, error) {
		if main.ID != "" {
			return scope, MainConversationService.ValidateRouterActor(ctx, main, scope.WakerID)
		}
		if entry != execution.ConversationRuleTeamEntryPoint {
			return scope, nil
		}
		thread, exists, err := ChannelThreadControllerManager.FindByChatID(ctx, owner.ID)
		if err != nil {
			return schedule.RuleScope{}, err
		}
		if !exists || thread.LeadChatID != owner.ID || thread.LeadWakerID != owner.WakerID {
			return schedule.RuleScope{}, status_error.AgentContextUnauthorized
		}
		scoped := scope
		scoped.ChannelID = thread.ChannelID
		return scoped, nil
	})
	if ChatService == nil {
		return bindings
	}
	return ChatService.BindManagedResourceChanges(owner, executionID, bindings)
}

func readAutomaticTaskReport(ctx context.Context, run models.AutomaticRun) (string, error) {
	if run.ThreadID == "" {
		messages, err := ChatService.ReadExecutionMessages(ctx, run.ChatID, run.ExecutionID)
		if err != nil {
			return "", err
		}
		for _, message := range messages {
			if message.ID == run.ExecutionID && message.Role == constants.MessageRoleAssistant {
				return automaticReportText(message), nil
			}
		}
		return "", nil
	}
	// 团队负责人可能先派工再汇总，只选择此运行实际产生的最后一条成功答复。
	for before := int64(0); ; {
		page, err := ChatService.ReadMessageTimeline(ctx, run.ChatID, before, 0, 40)
		if err != nil {
			return "", err
		}
		for i := len(page.Resources) - 1; i >= 0; i-- {
			message := page.Resources[i]
			if message.Role != constants.MessageRoleAssistant || message.Inherited {
				continue
			}
			outcome, err := ChatService.ReadExecution(ctx, run.ChatID, message.ID)
			if util.HasStatusCode(err, status_error.ExecutionNotFound) {
				continue
			}
			if err != nil {
				return "", err
			}
			if outcome.AutomaticRunID == run.ID && outcome.Status == constants.ExecutionStatusCompleted {
				return automaticReportText(message), nil
			}
		}
		if !page.HasMore || page.NextBeforeSequence <= 0 || page.NextBeforeSequence == before {
			return "", nil
		}
		before = page.NextBeforeSequence
	}
}

func automaticReportText(message messageview.Message) string {
	parts := make([]string, 0, len(message.DisplayParts))
	for _, part := range message.DisplayParts {
		if part.Type == constants.MessageDisplayPartText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func publishAutomaticTaskReport(ctx context.Context, run models.AutomaticRun, content string) (string, error) {
	chinese := strings.ContainsFunc(run.Configuration.Instruction, func(r rune) bool { return unicode.Is(unicode.Han, r) })
	if run.Source == constants.AutomaticRunSourceEventTest {
		content = localizedRuleMessage(chinese, "**测试运行**", "**Test run**") + "\n\n" + content
	}
	if run.SourceEvent != nil {
		source, err := url.Parse(run.SourceEvent.URL)
		if err == nil && source.Scheme == "https" && source.Host == "github.com" && source.User == nil {
			content += "\n\n[GitHub](" + source.String() + ")"
		}
	}
	if run.Configuration.ReportDestination == constants.AutomaticReportDestinationTopic {
		return publishAutomaticTopicReport(ctx, run, content)
	}
	kind, subject := "waker", run.WakerID
	if run.Configuration.ChannelID != "" {
		kind, subject = "team", run.Configuration.ChannelID
	}
	var message models.MainConversationMessage
	var err error
	if run.Source == constants.AutomaticRunSourceEvent && run.Configuration.Event != nil && run.Configuration.Event.TrackDiscussion && kind == "waker" {
		// A fallback run keeps the default standalone report if the original work was unavailable.
		if _, sourceErr := MainConversationService.Source(ctx, kind, subject, run.ChatID); sourceErr == nil {
			message, err = MainConversationService.PublishTrackedEventReport(ctx, run, content)
		} else if !util.HasStatusCode(sourceErr, status_error.ChatNotFound) {
			return "", sourceErr
		}
	}
	if message.ID == "" && err == nil {
		message, err = MainConversationService.PublishAutomaticReport(ctx, kind, subject, run.WakerID, run.ID, content)
	}
	if err != nil {
		return "", err
	}
	return "/chat/main/" + kind + "/" + subject + "?message=" + message.ID, nil
}

func publishAutomaticTopicReport(ctx context.Context, run models.AutomaticRun, content string) (string, error) {
	identity := requestidempotency.Identity{ID: run.TaskID}
	topicID := identity.ResourceID("automatic-report", "topic")
	topic, err := ChatService.GetChatSummary(ctx, topicID)
	if util.HasStatusCode(err, status_error.ChatNotFound) {
		layout, err := chatstorage.NewLayout(Config.ManagedFilesDirectory)
		if err != nil {
			return "", err
		}
		workspace, err := layout.AgentWorkspace(run.WakerID)
		if err != nil {
			return "", err
		}
		created, err := ChatService.CreateDormantChats(ctx, []chat.DormantChat{{Chat: models.Chat{
			ID: topicID, WakerID: run.WakerID, Title: run.Configuration.Name, WorkspaceDirectory: workspace,
			AdditionalWorkingDirectories: []string{workspace}, HiddenFromWakerHistory: true,
		}}})
		if err != nil {
			return "", err
		}
		topic.Chat = created[0]
	} else if err != nil {
		return "", err
	}
	if !topic.Chat.HiddenFromWakerHistory {
		return "", status_error.AutomaticTaskServiceInvalid
	}
	messageID := schedule.AutomaticReportMessageID(run)
	err = ChatService.AppendParticipantMessages(ctx, topicID, []chat.ParticipantMessageInput{{
		ID: messageID, Role: constants.MessageRoleAssistant, Actor: constants.AutomaticTaskActor,
		Content: content, SourceWakerID: run.WakerID, SourceChatID: run.ChatID,
	}})
	if err != nil {
		return "", err
	}
	return "/chats/" + topicID + "?wakerId=" + topic.Chat.WakerID, nil
}
