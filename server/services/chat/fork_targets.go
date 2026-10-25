package chat

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// ForkTarget 是不包含原生句柄的回合分叉入口状态。
type ForkTarget struct {
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
	target ForkTarget
	// configuration 是来源回合创建时的执行设置。
	configuration eventlog.ExecutionCreatedData
	// invocation 是该回合最后确认的原生调用终态。
	invocation eventlog.InvocationFinishedData
	// end 是复制权威日志前缀时使用的半开记录下标。
	end int
}

// ForkTargets 用权威事实计算可用性，旧的无定位回合保持禁用。
func (m *service) ForkTargets(ctx context.Context, chatID string) ([]ForkTarget, error) {
	records, state, err := m.loadChatState(chatID)
	if err != nil {
		return nil, err
	}
	points, err := forkPoints(records, state)
	if err != nil {
		return nil, err
	}
	result := make([]ForkTarget, 0, len(points))
	for _, point := range points {
		result = append(result, point.target)
	}
	return result, nil
}

func forkPoints(records []eventlog.Record, state chatState) ([]forkPoint, error) {
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
			if inherited.ForkInvocation != nil && inherited.ForkConfiguration != nil {
				indexes[record.MessageID] = len(points)
				points = append(points, forkPoint{target: ForkTarget{ExecutionID: record.MessageID}, configuration: *inherited.ForkConfiguration, invocation: *inherited.ForkInvocation, end: i + 1})
				lastExecution = record.MessageID
			}
		case constants.ChatEventTypeExecutionCreated:
			var config eventlog.ExecutionCreatedData
			if json.Unmarshal(record.Data, &config) != nil {
				return nil, status_error.ChatLogCorrupted
			}
			indexes[record.ExecutionID] = len(points)
			points = append(points, forkPoint{target: ForkTarget{ExecutionID: record.ExecutionID}, configuration: config})
			lastExecution = record.ExecutionID
		case constants.ChatEventTypeInvocationFinished:
			if index, ok := indexes[record.ExecutionID]; ok {
				if json.Unmarshal(record.Data, &points[index].invocation) != nil {
					return nil, status_error.ChatLogCorrupted
				}
			}
		case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			if index, ok := indexes[record.ExecutionID]; ok {
				points[index].end = i + 1
			}
		}
	}
	for i := range points {
		point := &points[i]
		point.target.Latest = point.target.ExecutionID == lastExecution
		switch {
		case state.threadWorkspaceID != "":
			point.target.ReasonCode = "team"
		case state.chat.ActiveExecutionID != "" || state.chat.WaitingApproval || state.chat.WaitingElicitation:
			point.target.ReasonCode = "busy"
		case point.end == 0:
			point.target.ReasonCode = "unfinished"
		case point.invocation.SessionOutputID == "":
			point.target.ReasonCode = "session_missing"
		case point.invocation.ForkMode == acpmeta.ForkTurn && point.invocation.ForkPosition != "":
			point.target.Available = true
		case point.invocation.ForkMode == acpmeta.ForkLatest && point.target.Latest:
			session := state.runtimeSessions[point.configuration.RuntimeID]
			if session.SessionID == point.invocation.SessionOutputID && session.LastExecutionID == point.target.ExecutionID {
				point.target.Available = true
			} else {
				point.target.ReasonCode = "session_changed"
			}
		case point.invocation.ForkMode == acpmeta.ForkLatest:
			point.target.ReasonCode = "latest_only"
		default:
			point.target.ReasonCode = "position_missing"
		}
	}
	return points, nil
}
