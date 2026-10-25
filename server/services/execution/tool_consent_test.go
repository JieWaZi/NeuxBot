package execution

import (
	"context"

	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// consentTestJournal 用真实 JSONL 测试许可事实，仅省略与许可无关的执行端口。
type consentTestJournal struct {
	// runtimeEventTestJournal 提供真实记录读取及未使用端口的断言。
	*runtimeEventTestJournal
	// updates 把许可请求投影交给测试中的用户响应。
	updates chan []models.PendingElicitation
}

func (j *consentTestJournal) AppendElicitationFact(chatID string, input eventlog.AppendInput) (eventlog.Record, error) {
	records, err := j.store.Append(chatID, []eventlog.AppendInput{input})
	if err != nil {
		return eventlog.Record{}, err
	}
	return records[0], nil
}

func (j *consentTestJournal) PublishElicitationFact(_ context.Context, _ eventlog.Record, pending []models.PendingElicitation) {
	j.updates <- pending
}
