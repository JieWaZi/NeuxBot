package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// consentTestJournal 用真实 JSONL 测试许可事实，仅省略与许可无关的执行端口。
type consentTestJournal struct {
	// interactionTestJournal 提供真实记录读取及未使用端口的断言。
	*interactionTestJournal
	// updates 把许可请求投影交给测试中的用户响应。
	updates chan eventlog.Record
}

func (j *consentTestJournal) AppendInteractionFact(chatID string, input eventlog.AppendInput) (eventlog.Record, error) {
	records, err := conversationfixture.CommitFacts(context.Background(), j.store, chatID, []eventlog.AppendInput{input})
	if err != nil {
		return eventlog.Record{}, err
	}
	return records[0], nil
}

func (j *consentTestJournal) PublishInteractionFact(_ context.Context, record eventlog.Record) {
	j.updates <- record
}

// interactionTestJournal observes exact reads on a real temporary journal.
type interactionTestJournal struct {
	// Journal is the unused business port in the interaction test.
	chatservice.Journal
	// store owns the test's business facts.
	store *conversation.Store
	// fullReads counts accidental scans outside the requested execution.
	fullReads int
	// executionRecords records the size of the exact execution read.
	executionRecords int
}

func (j *interactionTestJournal) ReadRecords(chatID string) ([]eventlog.Record, error) {
	j.fullReads++
	return conversationfixture.ReadFacts(context.Background(), j.store, chatID)
}

func (j *interactionTestJournal) ReadExecutionRecords(chatID, executionID string) ([]eventlog.Record, error) {
	rows, err := j.store.ReadTurn(context.Background(), chatID, executionID)
	records := conversationfixture.Records(chatID, rows)
	j.executionRecords = len(records)
	return records, err
}
