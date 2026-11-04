package eventlog_next

import (
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
)

// legacyRecord is the existing schema-1 disk envelope. Its identity scope is implicit.
type legacyRecord struct {
	// Version is the legacy JSONL schema.
	Version int `json:"schemaVersion"`
	// At is the original Unix milliseconds.
	At int64 `json:"recordedAt"`
	// Type is the old business fact type.
	Type constants.ChatEventType `json:"type"`
	// MessageID is explicit at message starts.
	MessageID string `json:"messageId"`
	// ExecutionID starts an execution scope, except for detached file changes.
	ExecutionID string `json:"executionId"`
	// InvocationID changes the current invocation scope.
	InvocationID string `json:"invocationId"`
	// Data holds the old unmodified payload or compact runtime content.
	Data json.RawMessage `json:"data"`
}

// legacyScope is bounded to the execution being decoded.
type legacyScope struct {
	// execution is the current execution scope.
	execution string
	// invocation is the current invocation scope.
	invocation string
	// start is the cursor at which this scope starts.
	start int64
	// tools holds only the most recent payload needed for each active tool patch.
	tools map[string]json.RawMessage
}

type legacyRuntimeMessage struct {
	// Content preserves the original event ordering.
	Content []legacyRuntimeContent `json:"content"`
	// Revision preserves the old projection's message revision.
	Revision int64 `json:"messageRevision"`
}

type legacyRuntimeContent struct {
	// Type identifies the runtime event.
	Type constants.RuntimeEventType `json:"type"`
	// Data contains the unpatched event payload.
	Data json.RawMessage `json:"data,omitempty"`
	// Patch reconstructs an event from the preceding snapshot.
	Patch *legacyToolPatch `json:"patch,omitempty"`
}

type legacyToolPatch struct {
	// ToolID identifies the tool whose snapshot is patched.
	ToolID string `json:"toolCallId"`
	// Base is the expected size of that snapshot.
	Base int `json:"baseBytes"`
	// Prefix is the unmodified prefix length.
	Prefix int `json:"prefixBytes"`
	// Suffix is the unmodified suffix length.
	Suffix int `json:"suffixBytes"`
	// Insert supplies the replacement bytes between the prefix and suffix.
	Insert []byte `json:"insert"`
}

func decodeLegacy(data []byte, cursor int64, scope *legacyScope) (agent.ChatFact, int64, int64, error) {
	var old legacyRecord
	if json.Unmarshal(data, &old) != nil || old.Version != constants.ChatLogSchemaVersion || old.Type == "" {
		return agent.ChatFact{}, 0, 0, status_error.ChatLogCorrupted
	}
	fact := agent.ChatFact{Type: old.Type, MessageID: old.MessageID, ExecutionID: old.ExecutionID, Data: old.Data}
	if old.Type == constants.ChatEventTypeFileChanges {
		if old.MessageID == "" || old.ExecutionID == "" {
			return agent.ChatFact{}, 0, 0, status_error.ChatLogCorrupted
		}
		return fact, old.At, cursor, nil
	}
	if old.ExecutionID != "" {
		*scope = legacyScope{execution: old.ExecutionID, start: cursor}
	}
	if old.Type == constants.ChatEventTypeExecutionCreated && old.ExecutionID == "" {
		return agent.ChatFact{}, 0, 0, status_error.ChatLogCorrupted
	}
	if old.InvocationID != "" && old.InvocationID != scope.invocation {
		scope.invocation, scope.tools = old.InvocationID, nil
	}
	fact.ExecutionID, fact.InvocationID = scope.execution, scope.invocation
	start := scope.start
	if start == 0 {
		start = cursor
	}
	switch old.Type {
	case constants.ChatEventTypeExecutionCreated:
		fact.MessageID = scope.execution
	case constants.ChatEventTypeAssistantMessage, constants.ChatEventTypeToolMessage:
		fact.MessageID = scope.execution
		if err := scope.expand(&fact); err != nil {
			return agent.ChatFact{}, 0, 0, err
		}
	case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
		*scope = legacyScope{}
	}
	return fact, old.At, start, nil
}

func toolID(data []byte) string {
	var id struct {
		// ID is the stable identity used by the schema-1 encoder.
		ID string `json:"toolCallId"`
	}
	_ = json.Unmarshal(data, &id)
	return id.ID
}

func (s *legacyScope) expand(fact *agent.ChatFact) error {
	var message legacyRuntimeMessage
	if json.Unmarshal(fact.Data, &message) != nil {
		return status_error.ChatLogCorrupted
	}
	for i := range message.Content {
		item := &message.Content[i]
		if p := item.Patch; p != nil {
			base := s.tools[p.ToolID]
			if item.Type != constants.RuntimeEventTypeToolUpdate || len(item.Data) != 0 ||
				p.ToolID == "" || len(base) == 0 || len(base) != p.Base ||
				p.Prefix < 0 || p.Suffix < 0 || p.Prefix > len(base) || p.Suffix > len(base)-p.Prefix {
				return status_error.ChatLogCorrupted
			}
			item.Data = append(append(append(json.RawMessage(nil), base[:p.Prefix]...), p.Insert...), base[len(base)-p.Suffix:]...)
			if !json.Valid(item.Data) || toolID(item.Data) != p.ToolID {
				return status_error.ChatLogCorrupted
			}
			item.Patch = nil
		}
		id := toolID(item.Data)
		switch item.Type {
		case constants.RuntimeEventTypeToolUpdate:
			if id != "" {
				if s.tools == nil {
					s.tools = make(map[string]json.RawMessage)
				}
				s.tools[id] = item.Data
			}
		case constants.RuntimeEventTypeToolResult:
			delete(s.tools, id)
		}
	}
	data, err := json.Marshal(message)
	if err != nil {
		return status_error.ChatLogCorrupted
	}
	fact.Data = data
	return nil
}
