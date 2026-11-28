package mainconversation

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const routedWorkScopePrefix = "ticket:"

// DefaultWorkScope identifies a subject's ordinary, persistent conversation with its current partner.
const DefaultWorkScope = models.MainConversationDefaultScope

// RoutedWorkScopeKey 为独立交办生成专属工作 Chat 的范围标识。
func RoutedWorkScopeKey(publicMessageID, scope string) string {
	return routedWorkScopePrefix + publicMessageID + ":" + scope
}

// RoutedWorkTicketID 从已核对的专属来源范围恢复原工单身份。
func RoutedWorkTicketID(scope string) string {
	value, routed := strings.CutPrefix(scope, routedWorkScopePrefix)
	if ticketID, _, hasScope := strings.Cut(value, ":"); routed && hasScope {
		return ticketID
	}
	return ""
}

// ReadRoutedPublication 在恢复路由前核对该输入是否已有直答回执。
func (s *Service) ReadRoutedPublication(ctx context.Context, input models.MainConversationInput) (bool, error) {
	message, err := s.Message(ctx, input.ConversationID, input.PublicMessageID)
	if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	newDigest := RoutedInputDigest(input.PublicMessageID, input.InputDigest, input.PublicReplyID, false)
	continuedDigest := RoutedInputDigest(input.PublicMessageID, input.InputDigest, input.PublicReplyID, true)
	if message.SourceChatID != "" || message.Content != input.PublicContent || (message.InputDigest != newDigest && message.InputDigest != continuedDigest) {
		return false, status_error.CreateRequestConflict
	}
	return true, nil
}

// ValidateRouterActor 核对原会话和执行伙伴仍然匹配，供原生输入提交边界复核。
func (s *Service) ValidateRouterActor(ctx context.Context, conv models.MainConversation, actor string) error {
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		return s.checkRouterActor(tx, conv, actor)
	})
}

func (s *Service) checkRouterActor(ctx context.Context, conv models.MainConversation, actor string) error {
	currentActor, err := s.subjectActor(ctx, conv.SubjectType, conv.SubjectID)
	if err != nil {
		return err
	}
	if currentActor != actor {
		return status_error.ChatReplyTargetUnavailable
	}
	current, err := s.store.Get(ctx, conv.ID)
	if err != nil {
		return err
	}
	if current.SubjectType != conv.SubjectType || current.SubjectID != conv.SubjectID {
		return status_error.ChatReplyTargetUnavailable
	}
	return nil
}

// ProcessRoutedInput 处理一条已接纳输入，提交直接答复或交给新工作来源。
func (s *Service) ProcessRoutedInput(ctx context.Context, projects controllers.ProjectController, conv models.MainConversation, input models.MainConversationInput,
	model ProjectRoutingModel, freeze func(ctx context.Context, actor, projectID, workingDirectory string) (models.MainConversationRoutedSource, error),
	promote func(context.Context, models.MainConversationInput) error,
) error {
	actor := input.RouterWakerID
	if err := s.ValidateRouterActor(ctx, conv, actor); err != nil {
		return err
	}
	var quote models.MainConversationMessage
	var err error
	if input.PublicReplyID != "" {
		quote, err = s.Message(ctx, conv.ID, input.PublicReplyID)
		if util.HasStatusCode(err, status_error.ChatMessageNotFound) || (err == nil && (quote.SourceChatID != "" || quote.RemovedAt != 0 || quote.AdmissionPending)) {
			return status_error.ChatReplyTargetUnavailable
		}
		if err != nil {
			return err
		}
	}
	history, previous, questionID, err := s.RouterContext(ctx, conv.ID, 0, quote)
	if err != nil {
		return err
	}
	route, err := s.PlanInput(ctx, projects, actor, model, input.Input, input.StartWork, history, quote, previous)
	if err != nil {
		return err
	}
	input.InputDigest = RoutedInputDigest(input.PublicMessageID, input.InputDigest, input.PublicReplyID, route.ContinuePrevious)
	if route.ContinuePrevious {
		if previous == "" || questionID == "" {
			return status_error.ChatReplyTargetUnavailable
		}
		input.PublicReplyID = questionID
		input.Input.ContextInstructions = append(input.Input.ContextInstructions, UnresolvedAssignment(previous))
	}
	route, err = CompleteRuleRoute(ctx, route, input.Input.SubmissionID, input.PublicContent)
	if err != nil {
		return err
	}
	if route.Message != "" || len(route.AutomaticTaskReceipts) != 0 {
		_, err := s.PublishRoute(ctx, conv, actor, input.Input.SubmissionID, input.PublicReplyID, input.PublicContent, input.InputDigest, input.Input, route)
		return err
	}
	source, err := freeze(ctx, actor, route.ProjectID, route.WorkingDirectory)
	if util.HasStatusCode(err, status_error.ProjectNotFound) || util.HasStatusCode(err, status_error.ProjectSourceUnavailable) {
		question := ProjectRoute{Message: ProjectRoutingQuestion(input.Input.Content), Clarification: true}
		_, err := s.PublishRoute(ctx, conv, actor, input.Input.SubmissionID, input.PublicReplyID, input.PublicContent, input.InputDigest, input.Input, question)
		return err
	}
	if err != nil {
		return err
	}
	if conv.SubjectType == "team" {
		locked, release, err := teamwork.Acquire(ctx, conv.SubjectID)
		if err != nil {
			return err
		}
		defer release()
		ctx = locked
	}
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		work, err := s.ReserveWork(tx, conv.SubjectType, conv.SubjectID, actor, RoutedWorkScopeKey(input.PublicMessageID, source.ScopeKey))
		if err != nil {
			return err
		}
		if work.ConversationID != conv.ID {
			return status_error.ChatReplyTargetUnavailable
		}
		input.WorkChatID, input.StartWork = work.WorkChatID, true
		source.Acknowledgment = route.Acknowledgment
		input.RoutedSource = &source
		return promote(tx, input)
	})
}

// SourceScopeKey distinguishes the persistent Chat from a separately authorized task.
func SourceScopeKey(messageID, frozenScope string, shared bool) string {
	if shared {
		return DefaultWorkScope
	}
	return RoutedWorkScopeKey(messageID, frozenScope)
}

// PlanInput lets the Router answer, clarify, or request work without a language-specific intent gate.
func (s *Service) PlanInput(ctx context.Context, projectsStore controllers.ProjectController, actor string, model ProjectRoutingModel, input workinput.Input, startWork bool, history []models.MainConversationMessage, quote models.MainConversationMessage, previous string) (ProjectRoute, error) {
	partner, err := s.wakers.Get(ctx, actor)
	if err != nil {
		return ProjectRoute{}, err
	}
	// 先取得同一查询快照内的所有名称，再由 Router 限制模型可见候选。
	projects, err := projectsStore.ListByWaker(ctx, actor, "", -1, 0)
	if err != nil {
		return ProjectRoute{}, err
	}
	content := input.Content
	if input.AttachmentOnly || input.CodeReferenceOnly {
		content = ""
	}
	var quoteInput models.MainConversationMessage
	if quote.Role == string(constants.MessageRoleAssistant) && quote.SourceChatID == "" &&
		quote.OriginalWork == nil && quote.ReplyToMessageID != "" {
		original, err := s.Message(ctx, quote.ConversationID, quote.ReplyToMessageID)
		if err != nil && !util.HasStatusCode(err, status_error.ChatMessageNotFound) {
			return ProjectRoute{}, err
		}
		if original.Role == string(constants.MessageRoleUser) && original.SourceChatID == "" &&
			original.RemovedAt == 0 && !original.AdmissionPending && original.Sequence < quote.Sequence {
			quoteInput = original
		}
	}
	return RouteProject(ctx, model, ProjectRoutingInput{
		Content: content, Partner: partner, History: history, Quote: quote, QuoteInput: quoteInput, PreviousRequest: previous,
		Projects: projects, WorkRequested: startWork || HasWorkResources(input),
	})
}

// RouterContext 从最近的澄清或显式引用恢复待交办目标。只有本次输入可授予工作范围。
func (s *Service) RouterContext(ctx context.Context, conversationID string, before int64, quote models.MainConversationMessage) ([]models.MainConversationMessage, string, string, error) {
	history, _, err := s.Messages(ctx, conversationID, before, ProjectRoutingHistoryLimit)
	if err != nil {
		return nil, "", "", err
	}
	if quote.ID == "" && len(history) > 0 {
		quote = history[len(history)-1]
	}
	questionID := quote.ID
	for depth := 0; depth < ProjectRoutingHistoryLimit && isRouterQuestion(quote); depth++ {
		original, err := s.Message(ctx, conversationID, quote.ReplyToMessageID)
		if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
			break
		}
		if err != nil {
			return nil, "", "", err
		}
		if original.Role != string(constants.MessageRoleUser) || original.RemovedAt != 0 || original.AdmissionPending || original.SourceChatID != "" || original.Sequence >= quote.Sequence {
			break
		}
		if original.ReplyToMessageID == "" {
			return history, original.Content, questionID, nil
		}
		parent, err := s.Message(ctx, conversationID, original.ReplyToMessageID)
		if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
			return history, original.Content, questionID, nil
		}
		if err != nil {
			return nil, "", "", err
		}
		if !isRouterQuestion(parent) || parent.Sequence >= original.Sequence {
			return history, original.Content, questionID, nil
		}
		quote = parent
	}
	return history, "", "", nil
}

func isRouterQuestion(message models.MainConversationMessage) bool {
	return message.SourceChatID == "" && message.IsReadablePublication() && message.ReplyToMessageID != "" &&
		message.ID == routerReplyID(message.ReplyToMessageID, true)
}

func routerReplyID(userMessageID string, clarification bool) string {
	kind := "answer"
	if clarification {
		kind = "clarification"
	}
	return (requestidempotency.Identity{ID: userMessageID}).ResourceID("main-conversation-router-reply", kind)
}

// PublishRoute 在一次提交中发布无工作来源的问答，并保留同一发送的幂等回执。
func (s *Service) PublishRoute(ctx context.Context, conv models.MainConversation, actor, submissionID, replyID, content, digest string, input workinput.Input, route ProjectRoute) (models.MainConversationMessage, error) {
	var result models.MainConversationMessage
	if HasWorkResources(input) {
		// 没有来源日志可保存原输入时，调用方保留附件和能力选择。
		return result, status_error.MainConversationProjectRequired
	}
	if conv.SubjectType == "team" {
		locked, release, err := teamwork.Acquire(ctx, conv.SubjectID)
		if err != nil {
			return result, err
		}
		defer release()
		ctx = locked
	}
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		if err := s.checkRouterActor(tx, conv, actor); err != nil {
			return err
		}
		if replyID != "" {
			quote, err := s.store.Message(tx, conv.ID, replyID)
			if err != nil {
				return err
			}
			if quote.SourceChatID != "" || quote.RemovedAt != 0 || quote.AdmissionPending {
				return status_error.ChatReplyTargetUnavailable
			}
		}
		message := models.MainConversationMessage{
			ID: UserMessageID(conv.ID, submissionID), ConversationID: conv.ID, Role: string(constants.MessageRoleUser),
			Content: content, ReplyToMessageID: replyID, InputDigest: digest,
		}
		saved, replay, err := s.store.Append(tx, message, conv.SubjectType, conv.SubjectID)
		if err != nil {
			return err
		}
		result = saved
		if replay {
			return nil
		}
		_, _, err = s.store.Append(tx, models.MainConversationMessage{
			ID: routerReplyID(saved.ID, route.Clarification), ConversationID: conv.ID, Role: string(constants.MessageRoleAssistant),
			Content: route.Message, ReplyToMessageID: saved.ID, SourceWakerID: actor,
			AutomaticTaskReceipts: route.AutomaticTaskReceipts,
		}, conv.SubjectType, conv.SubjectID)
		return err
	})
	return result, err
}

// HasWorkResources 表示输入需要保留原附件和能力选择。
func HasWorkResources(input workinput.Input) bool {
	return len(input.Attachments) != 0 || len(input.CodeReferences) != 0 || len(input.SkillIDs) != 0 ||
		len(input.ConnectorIDs) != 0 || len(input.PluginIDs) != 0 || input.Workflow != nil
}

// RoutedInputDigest 区分普通新路由接纳与既有显式工作范围提交，重投以原回执为准。
func RoutedInputDigest(messageID, digest, requestedReply string, continued bool) string {
	mode := "new"
	if continued {
		mode = "continuation"
	}
	return (requestidempotency.Identity{ID: messageID}).ResourceID("main-conversation-router-input", digest, requestedReply, mode)
}

// UnresolvedAssignment 保留等待补齐工作范围的原任务，同时保持当前用户正文原样。
func UnresolvedAssignment(content string) workinput.Instruction {
	return workinput.Instruction{Name: constants.ChatReplyTargetContextName + " - original assignment", Content: content}
}

// PrivateSourceInstruction 标记当前输入的来源，并指引同一主会话的答复回投。
func PrivateSourceInstruction() workinput.Instruction {
	return workinput.Instruction{
		Name: "Input origin",
		Content: "Keep this work conversation's input and execution details here. " +
			"If it continues the main conversation or its work, use the granted conversations.messages.post tool (conversations_messages_post over MCP) to send the user-facing reply or outcome there, even for brief chat. " +
			"Respect requests to reply only here.",
	}
}

// DispatchAcknowledgmentInstruction avoids repeating the plan already provided by the Router.
func DispatchAcknowledgmentInstruction() workinput.Instruction {
	return workinput.Instruction{
		Name: "Public delivery for this turn",
		Content: "The Router already told the user what you will do first. Do not post another acknowledgment. " +
			"Publish each verified, independently useful result as soon as it is ready.",
	}
}
