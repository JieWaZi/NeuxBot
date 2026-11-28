package global

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
)

// WithMainConversationRuleManagement 给真实主会话的文本 Router 签发本主体规则能力。
func WithMainConversationRuleManagement(ctx context.Context, conv models.MainConversation, actor string, options execution.WorkOptions) context.Context {
	if ScheduleService == nil || DB == nil {
		return ctx
	}
	scope := schedule.RuleScope{
		WakerID: actor, RuntimeID: options.RuntimeID, AccountID: options.AccountID,
		Model: options.Model, Reasoning: options.Reasoning, Timezone: automaticRuleTimezone(),
		PermissionLevel: options.PermissionLevel, WorkspaceReadOnly: options.WorkspaceReadOnly,
	}
	if conv.SubjectType == "team" {
		scope.ChannelID = conv.SubjectID
	}
	bindings := ScheduleService.ConversationRuleBindings(controllers.NewAgentCommandController(DB), conv.ID, func(ctx context.Context) (schedule.RuleScope, error) {
		if err := MainConversationService.ValidateRouterActor(ctx, conv, actor); err != nil {
			return schedule.RuleScope{}, err
		}
		return scope, nil
	})
	return mainconversation.WithRuleRouter(ctx, scope.Timezone, func(ctx context.Context, submission, content string, commands []mainconversation.RuleRouteCommand) (string, []eventlog.AutomaticTaskReceipt, error) {
		lines := make([]string, 0, len(commands))
		var tasks []eventlog.AutomaticTaskReceipt
		isChinese := strings.ContainsFunc(content, func(r rune) bool { return unicode.Is(unicode.Han, r) })
		for i, command := range commands {
			requestID := (requestidempotency.Identity{ID: submission}).ResourceID("automatic-rule-command", conv.ID, strconv.Itoa(i))
			result, err := executeMainRuleCommand(ctx, bindings, scope, command, requestID)
			if err != nil {
				lines = append(lines, localizedRuleMessage(isChinese,
					"这项规则操作未完成，请检查规则名称及配置后重试。",
					"The rule action could not be completed. Check the rule name and settings and try again."))
				continue
			}
			data, _ := result.Data.(map[string]any)
			task, err := eventlog.DecodeAutomaticTaskReceipt(result.CommandID, data)
			if err != nil {
				return "", nil, err
			}
			if task != nil {
				tasks = append(tasks, *task)
			} else {
				lines = append(lines, formatMainRuleReceipt(command.Operation, result, isChinese))
			}
		}
		return strings.Join(lines, "\n\n"), tasks, nil
	})
}

func executeMainRuleCommand(ctx context.Context, bindings []capability.Binding, scope schedule.RuleScope, command mainconversation.RuleRouteCommand, requestID string) (capability.Result, error) {
	for _, binding := range bindings {
		if binding.Operation != command.Operation {
			continue
		}
		if command.Params == nil {
			command.Params = map[string]any{}
		}
		if command.TaskName != "" {
			if _, supplied := command.Params["taskId"]; supplied {
				return capability.Result{}, status_error.AgentInputInvalid
			}
			id, err := ScheduleService.FindConversationRuleID(ctx, scope, command.TaskName)
			if err != nil {
				return capability.Result{}, err
			}
			command.Params["taskId"] = id
		}
		operation, _ := capability.Find(binding.Operation)
		if err := operation.Validate(command.Params); err != nil {
			return capability.Result{}, err
		}
		return binding.Call(ctx, capability.Request{Operation: binding.Operation, RequestID: requestID, Params: command.Params})
	}
	return capability.Result{}, status_error.AgentOperationForbidden
}

func formatMainRuleReceipt(operation string, result capability.Result, chinese bool) string {
	switch data := result.Data.(type) {
	case schedule.ConversationRuleReceipt:
		return formatRuleSummary(data, chinese)
	case map[string]any:
		if values, ok := data["rules"].([]schedule.ConversationRuleReceipt); ok {
			if len(values) == 0 {
				return localizedRuleMessage(chinese, "当前没有规则。", "There are no rules for this conversation.")
			}
			lines := make([]string, 0, len(values))
			for _, item := range values {
				lines = append(lines, "- "+formatRuleSummary(item, chinese))
			}
			return strings.Join(lines, "\n")
		}
		if operation == capability.RuleDelete {
			return localizedRuleMessage(chinese, "已删除规则。", "Rule deleted.")
		}
		if href, ok := data["href"].(string); ok {
			name, _ := data["name"].(string)
			if name == "" {
				name = localizedRuleMessage(chinese, "自动任务", "automatic task")
			}
			link := ruleReceiptLink(name, href)
			return localizedRuleMessage(chinese, "已保存 "+link+"。", "Saved "+link+".")
		}
	}
	return localizedRuleMessage(chinese, "规则操作已完成。", "Rule action completed.")
}

func formatRuleSummary(receipt schedule.ConversationRuleReceipt, chinese bool) string {
	task := receipt.Task
	label := localizedRuleMessage(chinese, "自动任务", "Automatic task")
	if task == nil {
		return ruleReceiptLink(label, receipt.Href)
	}
	line := ruleReceiptLink(task.Name, receipt.Href)
	if task.Enabled {
		line += localizedRuleMessage(chinese, " · 已启用", " · Enabled")
	} else {
		line += localizedRuleMessage(chinese, " · 已暂停", " · Paused")
	}
	if task.Schedule.Timezone != "" {
		line += " · " + task.Schedule.Timezone
	}
	if task.LastRun != nil {
		line += localizedRuleMessage(chinese, " · 最近运行 ", " · Latest run ") + string(task.LastRun.State)
	}
	if len(receipt.Runs) > 0 {
		count := strconv.Itoa(len(receipt.Runs))
		line += localizedRuleMessage(chinese, " · 最近 "+count+" 次运行", " · "+count+" recent runs")
	}
	return line
}

func ruleReceiptLink(name, href string) string {
	escape := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ")
	return "[" + escape.Replace(name) + "](" + href + ")"
}

func localizedRuleMessage(chinese bool, zh, en string) string {
	if chinese {
		return zh
	}
	return en
}

func automaticRuleTimezone() string {
	valid := func(zone string) bool {
		if zone == "" || zone == "Local" {
			return false
		}
		_, err := time.LoadLocation(zone)
		return err == nil
	}
	if zone := strings.TrimPrefix(os.Getenv("TZ"), ":"); valid(zone) {
		return zone
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(filepath.ToSlash(target), "/zoneinfo/"); ok && valid(zone) {
			return zone
		}
	}
	if name := time.Local.String(); valid(name) {
		return name
	}
	// Windows 上无法核对 IANA 区域时由用户明确指定时区。
	return ""
}
