package messageview

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func (p *MessageViewBuilder) applyFileChanges(record eventlog.Record) (string, error) {
	index, exists := p.messageIndexes[record.MessageID]
	if !exists {
		return "", nil
	}
	var data eventlog.FileChangesData
	if json.Unmarshal(record.Data, &data) != nil || data.RecordID == "" || data.Version < 1 {
		return "", status_error.ChatLogCorrupted
	}
	message := &p.messages[index]
	if message.FileChangeRecord != nil && message.FileChangeRecord.RecordID == data.RecordID && message.FileChangeRecord.Version > data.Version {
		return "", nil
	}
	message.FileChangeRecord = &data
	message.Revision = max(message.Revision, record.Sequence)
	return message.ID, nil
}
