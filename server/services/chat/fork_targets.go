package chat

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// forkTarget 保存实际分叉操作的权威校验结果。
type forkTarget struct {
	// ExecutionID 是前端可以提交的已结束回合。
	ExecutionID string `json:"executionId"`
	// Available 表示当前空闲状态和原生定位允许执行分叉。
	Available bool `json:"available"`
	// ReasonCode 是由前端本地化的稳定不可用原因码。
	ReasonCode string `json:"reasonCode,omitempty"`
	// Latest 表示该回合位于会话末尾。
	Latest bool `json:"latest"`
}

// forkPoint 保存同一回合的冻结配置、原生位置和历史截断边界。
type forkPoint struct {
	// target 是对外可见的可用性。
	target forkTarget
	// configuration 是来源回合创建时的执行设置。
	configuration eventlog.ExecutionCreatedData
	// boundary 是规范原生结果或继承边界确认的原生位置。
	boundary agent.NativeForkBoundary
	// end 是复制权威日志前缀时使用的半开记录下标。
	end int
}

func (m *service) resolveForkPoints(ctx context.Context, chatID string, records []eventlog.Record, state chatState) ([]forkPoint, error) {
	var binding *agent.NativeBinding
	if journal, ok := m.logStore.(*conversationJournal); ok {
		var err error
		binding, err = journal.ReadBinding(ctx, chatID)
		if err != nil {
			return nil, err
		}
	}
	points, err := forkPoints(records)
	if err != nil {
		return nil, err
	}
	if journal, ok := m.logStore.(*conversationJournal); ok {
		for i := range points {
			if points[i].boundary.Session.SessionID != "" {
				continue
			}
			result, err := journal.nativeOutcome(ctx, chatID, points[i].target.ExecutionID)
			if err != nil {
				return nil, err
			}
			if result != nil {
				points[i].boundary = agent.NativeForkBoundary{Session: result.Key.Session, Mode: result.ForkMode, Position: result.ForkPosition}
				identity, err := journal.nativeForkHistoryIdentity(ctx, chatID, points[i].target.ExecutionID, *result)
				if err != nil {
					return nil, err
				}
				points[i].boundary.HistoryIdentity = identity
			}
		}
	}
	assessForkPoints(points, records, state, binding)
	return points, nil
}

func forkPoints(records []eventlog.Record) ([]forkPoint, error) {
	points := []forkPoint{}
	indexes := map[string]int{}
	lastExecution := ""
	for i, record := range records {
		switch record.Type {
		case constants.ChatEventTypeMessageInherited:
			var inherited messageview.InheritedMessage
			if json.Unmarshal(record.Data, &inherited) != nil {
				return nil, status_error.ChatLogCorrupted
			}
			if inherited.ForkBoundary != nil && inherited.ForkConfiguration != nil {
				indexes[record.MessageID] = len(points)
				points = append(points, forkPoint{target: forkTarget{ExecutionID: record.MessageID}, configuration: *inherited.ForkConfiguration, boundary: *inherited.ForkBoundary, end: i + 1})
				lastExecution = record.MessageID
			}
		case constants.ChatEventTypeExecutionCreated:
			var config eventlog.ExecutionCreatedData
			if json.Unmarshal(record.Data, &config) != nil {
				return nil, status_error.ChatLogCorrupted
			}
			indexes[record.ExecutionID] = len(points)
			points = append(points, forkPoint{target: forkTarget{ExecutionID: record.ExecutionID}, configuration: config})
			lastExecution = record.ExecutionID
		case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			if index, ok := indexes[record.ExecutionID]; ok {
				points[index].end = i + 1
			}
		}
	}
	for i := range points {
		points[i].target.Latest = points[i].target.ExecutionID == lastExecution
	}
	return points, nil
}

func assessForkPoints(points []forkPoint, records []eventlog.Record, state chatState, binding *agent.NativeBinding) {
	for i := range points {
		point := &points[i]
		point.target.Available, point.target.ReasonCode = false, ""
		switch {
		case state.threadWorkspaceID != "":
			point.target.ReasonCode = "team"
		case state.chat.ActiveExecutionID != "" || state.chat.WaitingApproval || state.chat.WaitingElicitation:
			point.target.ReasonCode = "busy"
		case point.end == 0:
			point.target.ReasonCode = "unfinished"
		case point.boundary.Session.SessionID == "":
			point.target.ReasonCode = "session_missing"
		case point.boundary.Mode == acpmeta.ForkTurn && point.boundary.Position != "":
			point.target.Available = true
		case point.boundary.Mode == acpmeta.ForkLatest && point.target.Latest:
			sessionID, executionID := "", ""
			if binding != nil && !binding.Cleared {
				sessionID, executionID = binding.Session.SessionID, state.chat.LastEndedExecutionID
				if executionID == "" && records[point.end-1].Type == constants.ChatEventTypeMessageInherited {
					executionID = point.target.ExecutionID
				}
			} else if binding == nil && records[point.end-1].Type == constants.ChatEventTypeMessageInherited {
				sessionID, executionID = point.boundary.Session.SessionID, point.target.ExecutionID
			}
			if sessionID == point.boundary.Session.SessionID && executionID == point.target.ExecutionID {
				point.target.Available = true
			} else {
				point.target.ReasonCode = "session_changed"
			}
		case point.boundary.Mode == acpmeta.ForkLatest:
			point.target.ReasonCode = "latest_only"
		default:
			point.target.ReasonCode = "position_missing"
		}
	}
}

// nativeForkHistoryIdentity reads the existing admission batch's binding, not the current session.
func (j *conversationJournal) nativeForkHistoryIdentity(ctx context.Context, chatID, executionID string, result agent.NativeResult) (string, error) {
	rows, err := j.ReadTurn(ctx, chatID, executionID)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		input := row.Change.Input
		if input == nil || input.Session != result.Key.Session || !slices.Contains(result.Turn.ConsumedInputIDs, input.AdapterInputID) || row.Cursor < 2 {
			continue
		}
		preceding, err := j.ReadChanges(ctx, chatID, row.Cursor-2, 2)
		if err != nil {
			return "", err
		}
		if len(preceding.Records) != 2 {
			return "", status_error.ChatLogCorrupted
		}
		before, admitted := preceding.Records[0], preceding.Records[1]
		binding := before.Change.Binding
		if before.Cursor != row.Cursor-1 || admitted.Cursor != row.Cursor || admitted.Change.Input == nil || admitted.Change.Input.HostInvocationID != input.HostInvocationID || binding == nil || binding.Cleared || binding.Session != input.Session {
			return "", status_error.ChatLogCorrupted
		}
		return binding.HistoryIdentity(), nil
	}
	return "", nil
}
