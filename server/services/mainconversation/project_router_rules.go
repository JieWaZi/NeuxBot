package mainconversation

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const projectRulePrompt = `
- For an explicit request to manage automatic rules, return {"action":"rules","ruleCommands":[{"operation":"routine.rules.create","params":{...}}]}. A rule does not run now. Do not claim it was saved yet. Use routine.rules.list or routine.rules.get to inspect. Creation requires name, full instruction and one schedule object: kind once/hourly/daily/weekly/monthly/cron, timezone IANA name, plus relevant minute/hour/weekday (Sunday 0, Monday 1)/dayOfMonth/cron/onceAt (local YYYY-MM-DDTHH:mm). Use the supplied current time and time zone if the user leaves time zone implicit. If a necessary time zone or time is unknown, clarify. reportDestination main_conversation or topic and reportPolicy always or changes_only are optional. Use routine.rules.update with a real taskId or an exact taskName, and just the requested fields. Pause with enabled=false, resume with enabled=true. Use routine.rules.delete only for an explicit request to permanently delete. For existing rules never invent an identifier. Up to three expressly requested actions can be returned in the same array.
- For a simple recurring check, schedule the partner with the full check instruction and changes_only when the user wants only changes. Clarify missing scope. Use the stated cadence or pick a reasonable checking interval and expose it in the saved schedule. Apply this equally to repositories, websites and other supported tools.
- For a subscription that requires authoring, selecting, or changing a workflow, dispatch work using the usual scope choices so the partner can save a workflow and then set up its schedule. The dispatch is for setting up the subscription. The workflow itself runs on schedule.
`

// RuleRouteCommand 是模型提出并交由宿主核验的规则命令。
type RuleRouteCommand struct {
	// Operation 是本主体规则端口中明确支持的操作。
	Operation string `json:"operation"`
	// Params 是由原生工具同一 Schema 校验的操作参数。
	Params map[string]any `json:"params"`
	// TaskName 是由当前主体范围解析的准确规则名称。
	TaskName string `json:"taskName,omitempty"`
}

// RuleRouteCommands 保存一次路由提出的规则操作。
type RuleRouteCommands []RuleRouteCommand

// RuleRouter 核验并执行规则命令，输出真实确认结果。
type RuleRouter func(context.Context, string, string, []RuleRouteCommand) (string, []eventlog.AutomaticTaskReceipt, error)

type ruleRouterKey struct{}

// WithRuleRouter 为当前路由签发规则入口并提供已确认的本机时区。
func WithRuleRouter(ctx context.Context, timezone string, execute RuleRouter) context.Context {
	return context.WithValue(ctx, ruleRouterKey{}, ruleRouterScope{timezone: timezone, execute: execute})
}

type ruleRouterScope struct {
	// timezone 是本机确认的 IANA 时区，无法确认时为空。
	timezone string
	// execute 仅可操作当前会话主体有权管理的规则。
	execute RuleRouter
}

func ruleScopeFromContext(ctx context.Context) ruleRouterScope {
	scope, _ := ctx.Value(ruleRouterKey{}).(ruleRouterScope)
	return scope
}

func ruleRouterTime(scope ruleRouterScope) map[string]string {
	return map[string]string{"nowUTC": time.Now().UTC().Format(time.RFC3339), "timezone": scope.timezone}
}

// CompleteRuleRoute 用已核验的领域结果替换路由建议。
func CompleteRuleRoute(ctx context.Context, route ProjectRoute, submissionID, userContent string) (ProjectRoute, error) {
	if route.RuleCommands == nil {
		return route, nil
	}
	scope := ruleScopeFromContext(ctx)
	if scope.execute == nil {
		return ProjectRoute{}, nil
	}
	message, tasks, err := scope.execute(ctx, submissionID, userContent, *route.RuleCommands)
	route.Message, route.AutomaticTaskReceipts, route.RuleCommands = message, tasks, nil
	return route, err
}

// PublishAutomaticTaskReceipt 将来自公开交办的规则结果交还主会话。
func (s *Service) PublishAutomaticTaskReceipt(ctx context.Context, chatID, executionID, wakerID string, task eventlog.AutomaticTaskReceipt) (bool, error) {
	work, found, err := s.store.FindWork(ctx, chatID)
	if err != nil || !found {
		return false, err
	}
	origin, err := s.store.FindWorkflowInput(ctx, work.ConversationID, chatID, executionID)
	if util.HasStatusCode(err, status_error.ChatReplyTargetUnavailable) {
		// 私有发送可以直达原生会话。仅有相同来源 Chat 不构成公开授权。
		return false, nil
	}
	if err != nil {
		return false, err
	}
	conv, err := s.store.Get(ctx, work.ConversationID)
	if err != nil {
		return false, err
	}
	err = s.tx.Transaction(ctx, func(ctx context.Context) error {
		_, _, err := s.append(ctx, models.MainConversationMessage{
			ID: publicMessageID(chatID, executionID, task.ReceiptID), ConversationID: conv.ID,
			Role: string(constants.MessageRoleAssistant), ReplyToMessageID: origin.ID,
			SourceChatID: chatID, SourceExecutionID: executionID, SourceWakerID: wakerID,
			AutomaticTaskReceipts: []eventlog.AutomaticTaskReceipt{task},
		}, conv.SubjectType, conv.SubjectID)
		return err
	})
	return true, err
}

// HasAutomaticTaskReceipt 判断原回合已通过结构化回执完成公开答复。
func (s *Service) HasAutomaticTaskReceipt(ctx context.Context, chatID, executionID string) (bool, error) {
	work, found, err := s.store.FindWork(ctx, chatID)
	if err != nil || !found {
		return false, err
	}
	return s.store.HasAutomaticTaskReceipt(ctx, work.ConversationID, chatID, executionID)
}
