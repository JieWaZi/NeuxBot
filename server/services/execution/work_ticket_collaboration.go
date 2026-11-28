package execution

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// DelegationTargetResolver 只接收主体和同一能力提交派生的子单标识。
type DelegationTargetResolver func(context.Context, string, string, string) (workticket.DelegationInput, error)

// StartDelegatedWork 以子单身份在目标自己的受管工作来源派发，重投复用原接纳。
func StartDelegatedWork(ctx context.Context, core Service, ticket models.WorkTicket, options WorkOptions, main MainConversationMessages) error {
	options.EntryPoint, options.PermissionLevel = MainConversationSourceEntryPoint, ticket.PermissionLevel
	var instructions []workinput.Instruction
	if ticket.DelegationContext != "" {
		instructions = append(instructions, workinput.Instruction{Name: "Delegated task context", Content: ticket.DelegationContext})
	}
	_, err := core.Start(ctx, WorkIntent{
		Chat:    ChatTarget{ChatID: ticket.WorkChatID, MainConversationSource: &CreateChatSpec{WakerID: ticket.OwnerWakerID}},
		Input:   WorkInput{Actor: constants.WorkTicketHostActor, SubmissionID: ticket.ID, Content: ticket.DelegationContent, ContextInstructions: instructions},
		Options: options, ExecutionID: ticket.ID, UserMessageID: collaborationSourceInputID(ticket.ID, ticket.WorkChatID), Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Ticket: WorkTicketInput{DelegationID: ticket.ID, Address: workticket.Address{Kind: ticket.OriginKind, ID: ticket.OriginID},
			OwnerKind: ticket.OwnerKind, OwnerID: ticket.OwnerID, PublicMessageID: ticket.ID, MainConversation: main},
	})
	return err
}

// ResumeDelegatingWork 只传原代次已经提交的直接结果。接纳事务同时消费这批返回。
func ResumeDelegatingWork(ctx context.Context, core Service, resume workticket.DelegationResume, previous models.Execution, main MainConversationMessages) error {
	ticket := resume.Ticket
	id := (requestidempotency.Identity{ID: previous.ID}).ResourceID(constants.WorkTicketResultsOperation, ticket.ID)
	instruction := "Continue the original work using the verified direct child results. Read complete reports with work tickets results --report-id."
	if resume.QuestionID != "" {
		id = workticket.DelegationQuestionExecutionID(ticket, resume.QuestionID)
		instruction = "Answer the verified question from your direct child. Use work tickets reply with its report ID. This work still awaits the child's final result."
	}
	return continuePrivateWorkTicket(ctx, core, ticket, previous, main, id, "", resume.QuestionID, instruction,
		workinput.Instruction{Name: "Direct delegated work reports", Content: delegationResumeContext(resume.Reports)})
}

func delegationResumeContext(reports []models.WorkTicketReport) string {
	const contextLimit = 24 << 10
	if len(reports) == 0 {
		return ""
	}
	limitPerReport := contextLimit / len(reports)
	var results strings.Builder
	for _, report := range reports {
		brief := report.Content
		for {
			encoded, _ := json.Marshal(map[string]any{"ticketId": report.TicketID, "reportId": report.ID, "status": report.Status,
				"content": brief, "truncated": len(brief) != len(report.Content)})
			if len(encoded)+1 <= limitPerReport || len(brief) == 0 {
				results.Write(encoded)
				results.WriteByte('\n')
				break
			}
			brief = strings.ToValidUTF8(brief[:len(brief)/2], "")
		}
	}
	return results.String()
}

// ResumeDelegatedQuestion applies an accepted business answer without resolving user elicitations or approvals.
func ResumeDelegatedQuestion(ctx context.Context, core Service, launch workticket.DelegationReplyLaunch, previous models.Execution, main MainConversationMessages) error {
	if launch.Reply.ExecutionID != previous.ID || launch.Reply.TicketID != launch.Ticket.ID || launch.Reply.ReplyToReportID == "" {
		return status_error.WorkTicketAttemptUnavailable
	}
	return continuePrivateWorkTicket(ctx, core, launch.Ticket, previous, main, launch.Reply.ID, launch.Reply.ID, "",
		"Continue your delegated work using the direct requester's answer.", workinput.Instruction{Name: "Reply to your delegated work question", Content: launch.Reply.Content})
}

func continuePrivateWorkTicket(ctx context.Context, core Service, ticket models.WorkTicket, previous models.Execution, main MainConversationMessages,
	id, replyID, questionID, instruction string, contextInstruction workinput.Instruction,
) error {
	options := WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: previous.RuntimeID, AccountID: previous.AccountID,
		Model: previous.Model, Reasoning: previous.Reasoning, Fast: previous.Fast, ContextWindow: previous.ContextWindow,
		ConfigurationResolved: true, PermissionLevel: previous.PermissionLevel, WorkspaceReadOnly: previous.WorkspaceReadOnly}
	if ticket.OriginKind == constants.WorkTicketOriginMain {
		options.EntryPoint = MainConversationSourceEntryPoint
	} else {
		main = nil
	}
	if ticket.Transferred {
		options.EntryPoint = MainConversationSourceEntryPoint
		noInheritedCapabilities := []string{}
		options.AllowedAgentDomains = &noInheritedCapabilities
	}
	_, err := core.Start(ctx, WorkIntent{
		Chat: ChatTarget{ChatID: ticket.WorkChatID}, Options: options,
		Input: WorkInput{Actor: constants.WorkTicketHostActor, SubmissionID: id, Content: instruction,
			ContextInstructions: []workinput.Instruction{contextInstruction}},
		ExecutionID: id, UserMessageID: collaborationSourceInputID(id, ticket.WorkChatID), Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Ticket: WorkTicketInput{ResumeTicket: ticket, DelegationReplyID: replyID, DelegationQuestionID: questionID, Address: workticket.Address{Kind: ticket.OriginKind, ID: ticket.OriginID},
			OwnerKind: ticket.OwnerKind, OwnerID: ticket.OwnerID, PublicMessageID: id, MainConversation: main},
	})
	return err
}

func collaborationSourceInputID(submissionID, chatID string) string {
	return (requestidempotency.Identity{ID: submissionID}).ResourceID("work-ticket-input", chatID)
}

func (turn *workTicketTurn) delegationBindings() []capability.Binding {
	if turn.resolveTarget == nil {
		return nil
	}
	receipt := func(child models.WorkTicket) capability.Result {
		return capability.Result{CommandID: child.ID, Data: map[string]any{"ticketId": child.ID}}
	}
	return []capability.Binding{{
		Operation: constants.WorkTicketDelegateOperation,
		Call: func(ctx context.Context, req capability.Request) (capability.Result, error) {
			kind, _ := req.Params["subjectType"].(string)
			id, _ := req.Params["subjectId"].(string)
			target, err := turn.resolveTarget(ctx, workticket.DelegationID(turn.binding, req.RequestID), kind, id)
			if err != nil {
				return capability.Result{}, err
			}
			target.RequestID = req.RequestID
			target.Content, _ = req.Params["content"].(string)
			target.Context, _ = req.Params["context"].(string)
			target.QuestionOnly, _ = req.Params["questionOnly"].(bool)
			child, err := turn.tickets.Delegate(ctx, turn.binding, target)
			if err != nil {
				return capability.Result{}, err
			}
			if turn.wake != nil {
				turn.wake()
			}
			return receipt(child), nil
		},
		Receipt: func(ctx context.Context, id string) (capability.Result, error) {
			child, err := turn.tickets.DelegationReceipt(ctx, turn.binding, id)
			if err != nil {
				return capability.Result{}, err
			}
			result := receipt(child)
			result.Replayed = true
			return result, nil
		},
	}, {
		Operation: constants.WorkTicketResultsOperation,
		Call: func(ctx context.Context, req capability.Request) (capability.Result, error) {
			id, _ := req.Params["reportId"].(string)
			reports, err := turn.tickets.DelegationReports(ctx, turn.binding, id)
			if err != nil {
				return capability.Result{}, err
			}
			entries := make([]map[string]any, 0, len(reports))
			for _, report := range reports {
				entries = append(entries, map[string]any{"reportId": report.ID, "ticketId": report.TicketID, "status": report.Status, "content": report.Content})
			}
			return capability.Result{Data: map[string]any{"reports": entries}}, nil
		},
	}, {
		Operation: constants.WorkTicketReplyOperation,
		Call: func(ctx context.Context, req capability.Request) (capability.Result, error) {
			id, _ := req.Params["reportId"].(string)
			content, _ := req.Params["content"].(string)
			reply, err := turn.tickets.ReplyToDelegation(ctx, turn.binding, req.RequestID, id, content)
			if err != nil {
				return capability.Result{}, err
			}
			if turn.wake != nil {
				turn.wake()
			}
			return capability.Result{CommandID: reply.ID, Data: map[string]any{"replyId": reply.ID}}, nil
		},
		Receipt: func(ctx context.Context, key string) (capability.Result, error) {
			reply, err := turn.tickets.DelegationReplyReceipt(ctx, turn.binding, key)
			if err != nil {
				return capability.Result{}, err
			}
			return capability.Result{CommandID: reply.ID, Replayed: true, Data: map[string]any{"replyId": reply.ID}}, nil
		},
	}}
}
