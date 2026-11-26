package chat

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

const reviewWorkbench = "workbench"

// ListReviewChatIDs 通过复盘接纳索引查找需要恢复的聊天。
func (m *service) ListReviewChatIDs(ctx context.Context) ([]string, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return nil, status_error.ChatLogRecordInvalid
	}
	return journal.Store.ListReviewChatIDs(ctx)
}

// reviewAdmission tags only trusted ordinary chat. The marker is private to the
// execution creation fact and is read only after a successful terminal.
func (m *service) reviewAdmission(ctx context.Context, turn StartedTurn) []agent.ConversationChange {
	changes := factsToChanges(turn.Facts)
	if m.freezeReview == nil || turn.Chat.HiddenFromWakerHistory {
		return changes
	}
	for _, change := range changes {
		if change.Fact == nil || change.Fact.Type != constants.ChatEventTypeExecutionCreated {
			continue
		}
		var wire map[string]json.RawMessage
		if json.Unmarshal(change.Fact.Data, &wire) != nil {
			continue
		}
		var entry string
		_ = json.Unmarshal(wire["entryPoint"], &entry)
		if entry != reviewWorkbench {
			continue
		}
		if admission := m.freezeReview(ctx, turn.Chat); admission != nil {
			wire["review"], _ = json.Marshal(admission)
			change.Fact.Data, _ = json.Marshal(wire)
		}
	}
	return changes
}

// ReadReviewTurnStarts 读取合格终态的位置和时间，无需读入聊天正文。
func (m *service) ReadReviewTurnStarts(ctx context.Context, chatID string, after, through int64, authority reviewstate.Admission, limit int) ([]reviewstate.Turn, error) {
	return m.readReviewTurns(ctx, chatID, after, through, authority, limit, false)
}

// ReadReviewTurns 按权威日志的持久终态读取已授权的输入证据。
func (m *service) ReadReviewTurns(ctx context.Context, chatID string, after, through int64, authority reviewstate.Admission, limit int) ([]reviewstate.Turn, error) {
	return m.readReviewTurns(ctx, chatID, after, through, authority, limit, true)
}

func (m *service) readReviewTurns(ctx context.Context, chatID string, after, through int64, authority reviewstate.Admission, limit int, readBodies bool) ([]reviewstate.Turn, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return nil, status_error.ChatLogRecordInvalid
	}
	turns := make([]reviewstate.Turn, 0, limit)
	pageSize := 128
	if readBodies {
		pageSize = max(32, limit)
	}
	for len(turns) < limit {
		starts, next, err := journal.ReadReviewStarts(ctx, chatID, after, through, pageSize)
		if err != nil {
			return nil, err
		}
		for _, start := range starts {
			turn := admittedReviewTurn(start, authority)
			if !turn.Memory && !turn.Skill {
				continue
			}
			if readBodies {
				turn, err = readReviewTurnBody(ctx, journal, chatID, start, turn)
				if err != nil {
					return nil, err
				}
				if !turn.Memory && !turn.Skill {
					continue
				}
			}
			turns = append(turns, turn)
			if len(turns) == limit {
				break
			}
		}
		if next == 0 {
			break
		}
		after = next
	}
	return turns, nil
}

func admittedReviewTurn(start conversation.ReviewStart, authority reviewstate.Admission) reviewstate.Turn {
	fact := start.Admission.Change.Fact
	var data struct {
		// Review 是接纳冻结的学习代次。
		Review reviewstate.Admission `json:"review"`
		// EntryPoint 是服务器可信入口。
		EntryPoint string `json:"entryPoint"`
		// UserMessageID 指向本轮已接纳的用户原文。
		UserMessageID string `json:"userMessageId"`
	}
	if fact == nil || json.Unmarshal(fact.Data, &data) != nil || data.EntryPoint != reviewWorkbench || data.UserMessageID == "" {
		return reviewstate.Turn{}
	}
	return reviewstate.Turn{
		UserID: data.UserMessageID, Sequence: start.Terminal, At: time.UnixMilli(start.CompletedAt),
		Memory: data.Review.Memory != 0 && data.Review.Memory == authority.Memory,
		Skill:  data.Review.Skill != 0 && data.Review.Skill == authority.Skill,
	}
}

func readReviewTurnBody(ctx context.Context, journal *conversationJournal, chatID string, start conversation.ReviewStart, turn reviewstate.Turn) (reviewstate.Turn, error) {
	user, err := journal.Store.ReadMessage(ctx, chatID, turn.UserID)
	if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return reviewstate.Turn{}, nil
	}
	if err != nil {
		return reviewstate.Turn{}, err
	}
	if user.Role != constants.MessageRoleUser || strings.TrimSpace(user.Text) == "" {
		return reviewstate.Turn{}, nil
	}
	turn.UserID, turn.User = user.ID, user.Text
	if !turn.Skill {
		return turn, nil
	}

	fact := start.Admission.Change.Fact
	assistant, checkpoint, err := journal.ReadCurrentMessageCheckpointThrough(ctx, chatID, fact.ExecutionID, start.Terminal)
	if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return turn, nil
	}
	if err != nil {
		return reviewstate.Turn{}, err
	}
	if assistant.ID != fact.ExecutionID || checkpoint == 0 {
		return turn, nil
	}
	builder := messageview.NewMessageViewBuilder()
	builder.SeedNativeLiveMessage(assistant.ID, assistant.Revision)
	if err := builder.ReplaceStoredContent(assistant, checkpoint, start.CompletedAt); err != nil {
		return reviewstate.Turn{}, err
	}
	message := builder.Messages()[0]
	turn.AssistantID, turn.Assistant = assistant.ID, message.AssistantResponseText(true)
	turn.Steps = reviewToolSteps(message, assistant.Parts)
	return turn, nil
}

func reviewToolSteps(message messageview.Message, parts []agent.MessagePart) []reviewstate.ToolStep {
	type toolEvidence struct {
		// Name 是原生稳定工具标识。
		Name string `json:"name"`
		// ToolName 是提供者显式提供的工具名。
		ToolName string `json:"toolName"`
		// ErrorCode 是结构化错误标识。
		ErrorCode string `json:"errorCode"`
		// ExitCode 是结构化命令退出码。
		ExitCode *int `json:"exitCode"`
	}
	observed := make(map[string]toolEvidence)
	for _, part := range parts {
		if part.ToolCallID == "" {
			continue
		}
		summary := observed[part.ToolCallID]
		if json.Unmarshal(part.Data, &summary) == nil {
			observed[part.ToolCallID] = summary
		}
	}
	steps := make([]reviewstate.ToolStep, 0)
	for _, part := range message.DisplayParts {
		if part.ToolStatus != constants.MessageToolStatusCompleted && part.ToolStatus != constants.MessageToolStatusFailed {
			continue
		}
		if part.ToolCategory == "" || part.ID == "" {
			continue
		}
		info := observed[part.ToolCallID]
		name := strings.TrimSpace(string(part.ToolCategory) + " " + string(part.ToolAction))
		identifier := info.Name
		if info.ToolName != "" {
			identifier = info.ToolName
		}
		if safeReviewToolIdentifier(identifier) {
			name += " (" + identifier + ")"
		}
		failed := part.ToolStatus == constants.MessageToolStatusFailed
		summary := ""
		switch {
		case failed && safeReviewToolIdentifier(info.ErrorCode):
			summary = "Tool error " + info.ErrorCode
		case failed && info.ExitCode != nil:
			summary = "Exit code " + strconv.Itoa(*info.ExitCode)
		case failed:
			summary = "Tool reported failure"
		}
		steps = append(steps, reviewstate.ToolStep{ID: part.ID, Name: name, Failed: failed, Summary: summary})
	}
	return steps
}

func safeReviewToolIdentifier(id string) bool {
	return len(id) > 0 && len(id) <= 64 && !strings.ContainsFunc(id, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	}) && reviewstate.Redact(id) == id
}

func (m *service) queueReviewDeletion(ids []string) {
	if m.reviewDeleted == nil {
		return
	}
	for _, id := range ids {
		m.reviewDeleted(id)
	}
}
