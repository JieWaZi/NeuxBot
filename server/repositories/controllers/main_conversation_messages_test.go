package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWorkPublicationsUseIndexedPageLookups(t *testing.T) {
	db := newTestDB(t)
	const turnCount = 500
	executions := make([]string, 0, turnCount)
	rows := make([]models.MainConversationMessage, 0, 2*turnCount)
	for i := range turnCount {
		execution := fmt.Sprintf("run-%d", i)
		executions = append(executions, execution)
		input := models.MainConversationMessage{ID: execution, Sequence: int64(2*i + 1), Role: "user", ConversationID: "main", SourceChatID: "work"}
		answer := models.MainConversationMessage{ID: "post-" + execution, Sequence: input.Sequence + 1, Role: "assistant", ConversationID: "main", SourceChatID: "work", ReplyToMessageID: execution, Content: "公开正文"}
		if i%2 == 0 {
			input.SourceExecutionID, answer.SourceExecutionID = execution, execution
		}
		rows = append(rows, input, answer)
	}
	if err := db.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	log := &repositoryQueryLog{Interface: logger.Discard}
	observed := db.Session(&gorm.Session{Logger: log})
	store := NewMainConversationController(observed)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(observed))
	work := models.MainConversationWork{WorkChatID: "work", ConversationID: "main"}
	for _, count := range []int{1, 100} {
		log.Statements = nil
		page := executions[len(executions)-count:]
		var publications []models.MainConversationMessage
		err := transactions.Transaction(t.Context(), func(tx context.Context) error {
			var err error
			publications, err = store.WorkPublications(tx, work, page)
			return err
		})
		if err != nil || len(publications) != count {
			t.Fatalf("%d executions returned %d publications: %v", count, len(publications), err)
		}
		for i, publication := range publications {
			if publication.SourceExecutionID != page[i] || publication.ID != "post-"+page[i] {
				t.Fatalf("wrong publication for execution %s: %+v", page[i], publication)
			}
		}
		if len(log.Statements) != 2 {
			t.Fatalf("%d executions used %d lookups, expected 2", count, len(log.Statements))
		}
		for i, expected := range [][]string{
			{"source_execution_id=?"},
			{"reply_to_message_id=?", "id=?"},
		} {
			var plan []struct {
				// Detail 是 SQLite 返回的查询计划步骤。
				Detail string
			}
			if err := db.Raw("EXPLAIN QUERY PLAN " + log.Statements[i]).Scan(&plan).Error; err != nil {
				t.Fatal(err)
			}
			searches := make(map[string]bool)
			for _, step := range plan {
				if strings.HasPrefix(step.Detail, "SCAN main_conversation_messages") {
					t.Fatalf("%d executions scanned the publication table: %+v", count, plan)
				}
				if strings.HasPrefix(step.Detail, "SEARCH ") {
					for _, predicate := range expected {
						if strings.HasSuffix(step.Detail, "("+predicate+")") {
							searches[predicate] = true
						}
					}
				}
			}
			if len(searches) != len(expected) {
				t.Errorf("%d executions: expected searches by %q, got %+v", count, expected, plan)
			}
		}
	}
}

func TestMainConversationFailureCandidatesUseIndexedPageLookups(t *testing.T) {
	db := newTestDB(t)
	const answerCount, unansweredCount = 950, 50
	var messages []models.MainConversationMessage
	var readable []models.ChatReadableMessage
	for i := range answerCount + unansweredCount {
		user, execution := fmt.Sprintf("user-%d", i), fmt.Sprintf("execution-%d", i)
		input := models.MainConversationMessage{ConversationID: "main", ID: user, Role: "user", Sequence: 2*int64(i) + 1, SourceChatID: "work", SourceExecutionID: execution}
		messages = append(messages, input)
		if i < answerCount {
			messages = append(messages, models.MainConversationMessage{ConversationID: "main", ID: "answer-" + user, Role: "assistant", Sequence: input.Sequence + 1, SourceChatID: "work", SourceExecutionID: execution, ReplyToMessageID: user})
		}
		readable = append(readable, models.ChatReadableMessage{ChatID: "work", MessageID: execution, ExecutionID: execution, OrderSequence: input.Sequence, Kind: "failure"})
	}
	if err := db.CreateInBatches(messages, 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(readable, 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.MainConversationWork{WorkChatID: "work", ConversationID: "main", WakerID: "original-member", ScopeKey: "managed"}).Error; err != nil {
		t.Fatal(err)
	}
	log := &repositoryQueryLog{Interface: logger.Discard}
	observed := db.Session(&gorm.Session{Logger: log})
	store := NewMainConversationController(observed)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(observed))
	var page []models.MainConversationMessage
	for _, message := range messages[2*(answerCount-50):] {
		if message.Role == "user" {
			page = append(page, message)
		}
	}
	for _, n := range []int{1, 100} {
		log.Statements = nil
		var failures map[string]MainConversationFailureCandidate
		err := transactions.Transaction(t.Context(), func(tx context.Context) error {
			var err error
			failures, err = store.UnansweredFailures(tx, "main", page[len(page)-n:])
			return err
		})
		if err != nil || len(failures) != min(n, unansweredCount) || failures["user-999"].WakerID != "original-member" {
			t.Fatalf("%d inputs, %d failures, last candidate %+v: %v", n, len(failures), failures["user-999"], err)
		}
		if len(log.Statements) != 3 {
			t.Fatalf("%d inputs used %d lookups, expected 3", n, len(log.Statements))
		}
		for i, key := range []string{"reply_to_message_id=?", "chat_id=? AND message_id=?", "work_chat_id=?"} {
			var plan []struct {
				// Detail 是 SQLite 返回的扫描或索引访问说明。
				Detail string
			}
			if err := db.Raw("EXPLAIN QUERY PLAN " + log.Statements[i]).Scan(&plan).Error; err != nil {
				t.Fatal(err)
			}
			for _, step := range plan {
				if strings.HasPrefix(step.Detail, "SEARCH ") && strings.Contains(step.Detail, key) {
					key = ""
					break
				}
			}
			if key != "" {
				t.Errorf("%d inputs did not use lookup by %s: %+v", n, key, plan)
			}
		}
	}
}
