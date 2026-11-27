package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMainActivityKeepsOriginalSubjectAcrossSourceTurns(t *testing.T) {
	ctx := t.Context()
	hub := events.New()
	defer hub.Close()
	db := WithEventPublisher(newTestDB(t), hub)
	for _, row := range []any{
		&models.Waker{ID: "member", Name: "伙伴"},
		&models.Channel{ID: "team-id", Name: "团队", LeadWakerID: "member"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := NewMainConversationController(db)
	team, err := store.Ensure(ctx, "team", "team-id")
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.ReserveWork(ctx, team.ID, "member", "managed")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Chat{ID: source.WorkChatID, WakerID: "member", HiddenFromWakerHistory: true}).Error; err != nil {
		t.Fatal(err)
	}
	changes := hub.Subscribe(constants.MainConversationEventTopic, eventsResource("team", "team-id"))
	defer changes.Close()
	entities := hub.Subscribe("entities", eventsResource("channel", "team-id"))
	defer entities.Close()
	board := hub.Subscribe("work-board", eventsResource("global", ""))
	defer board.Close()
	for i, state := range []struct {
		// execution 是持久接纳的工作标识。
		execution string
		// approval 是本步是否等待审批。
		approval bool
		// elicitation 是本步是否等待补充输入。
		elicitation bool
	}{{"run", false, false}, {"run", true, false}, {"run", true, true}, {"run", false, true}, {"run", false, false}, {"", false, false}} {
		approvals, elicitations := "[]", "[]"
		if state.approval {
			approvals = `[{"approvalId":"approve"}]`
		}
		if state.elicitation {
			elicitations = `[{"elicitationId":"question"}]`
		}
		if err := NewChatController(db).AdvanceProjection(ctx, source.WorkChatID, int64(i), map[string]any{"latest_sequence": int64(i + 1), "active_execution_id": state.execution, "waiting_approval": state.approval, "pending_approvals": approvals, "waiting_elicitation": state.elicitation, "pending_elicitations": elicitations}); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ActiveWork(ctx, team.ID)
		if err != nil {
			t.Fatal(err)
		}
		active := state.execution != ""
		if (len(rows) == 1) != active || active && (rows[0].SourceWakerID != "member" || rows[0].WorkChatID != source.WorkChatID || rows[0].ExecutionID != state.execution || rows[0].WaitingApproval != state.approval || rows[0].WaitingElicitation != state.elicitation) {
			t.Fatalf("source activity: %+v", rows)
		}
		facts, err := NewWorkBoardController(db).ExecutorAttentionFacts(ctx, "local", []string{"team-id", "member"}, nil)
		summary := facts.MainWorkCounts["team-id"]
		var approval, elicitation bool
		for _, request := range facts.Requests {
			if request.OwnerKind != constants.WorkOwnerKindMainConversation || request.OwnerID != team.ID || request.ExecutorID != "team-id" || request.ChatID != source.WorkChatID || request.ExecutionID != state.execution {
				t.Fatalf("active request lost its original source: %+v", request)
			}
			approval = approval || request.Kind == constants.WorkBoardRequestKindApproval
			elicitation = elicitation || request.Kind == constants.WorkBoardRequestKindInput
		}
		if err != nil || len(facts.Chats) != 0 || summary.SubjectType != "team" || (summary.RunningCount == 1) != (active && !state.approval && !state.elicitation) ||
			approval != state.approval || elicitation != state.elicitation || facts.MainWorkCounts["member"].RunningCount != 0 {
			t.Fatalf("sidebar ownership: %+v %v", facts, err)
		}

		select {
		case <-changes.Events:
		default:
			t.Fatal("missing main conversation activity change")
		}
		select {
		case <-entities.Events:
		default:
			t.Fatal("missing team activity change")
		}
		select {
		case <-board.Events:
		default:
			t.Fatal("missing global board change")
		}
	}
}

func TestMainActiveWorkPublicationsUseOneIndexedBatch(t *testing.T) {
	db := newTestDB(t)
	const activeCount = 100
	ids := make([]string, 0, activeCount)
	chats := make([]models.Chat, 0, activeCount)
	sources := make([]models.MainConversationWork, 0, activeCount)
	var messages []models.MainConversationMessage
	for i := range activeCount {
		id := fmt.Sprintf("work-%03d", i)
		ids = append(ids, id)
		chats = append(chats, models.Chat{ID: id, WakerID: "partner", HiddenFromWakerHistory: true})
		sources = append(sources, models.MainConversationWork{
			WorkChatID: id, ConversationID: "main", WakerID: "partner", ScopeKey: id,
		})
		messages = append(messages, models.MainConversationMessage{
			ID: "input-" + id, ConversationID: "main", Role: string(constants.MessageRoleUser),
			Sequence: int64(2*i + 1), SourceChatID: id, SourceExecutionID: "execution-" + id,
		})
		if i%2 == 1 {
			messages = append(messages, models.MainConversationMessage{
				ID: "reply-" + id, ConversationID: "main", Role: string(constants.MessageRoleAssistant),
				Sequence: int64(2*i + 2), SourceChatID: id, SourceExecutionID: "execution-" + id,
			})
		}
	}
	for i := range 1000 {
		messages = append(messages, models.MainConversationMessage{
			ID: fmt.Sprintf("historical-%d", i), ConversationID: "main", Role: string(constants.MessageRoleAssistant),
			Sequence: int64(2*activeCount + i + 1), SourceChatID: ids[0], SourceExecutionID: fmt.Sprintf("past-%d", i),
		})
	}
	messages = append(messages,
		models.MainConversationMessage{ID: "different-source", ConversationID: "main", Sequence: 1201, Role: string(constants.MessageRoleAssistant), SourceChatID: ids[1], SourceExecutionID: "execution-" + ids[0]},
		models.MainConversationMessage{ID: "different-conversation", ConversationID: "other", Sequence: 1, Role: string(constants.MessageRoleAssistant), SourceChatID: ids[0], SourceExecutionID: "execution-" + ids[0]},
	)
	for _, rows := range []any{chats, sources, messages} {
		if err := db.CreateInBatches(rows, 50).Error; err != nil {
			t.Fatal(err)
		}
	}

	log := &repositoryQueryLog{Interface: logger.Discard}
	observed := db.Session(&gorm.Session{Logger: log})
	store := NewMainConversationController(observed)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(observed))
	for _, count := range []int{1, activeCount} {
		if err := db.Model(&models.Chat{}).Where("id IN ?", ids[:count]).
			UpdateColumn("active_execution_id", gorm.Expr("? || id", "execution-")).Error; err != nil {
			t.Fatal(err)
		}
		log.Statements = nil
		var active []models.MainConversationActivity
		err := transactions.Transaction(t.Context(), func(tx context.Context) error {
			var err error
			active, err = store.ActiveWork(tx, "main")
			return err
		})
		if err != nil || len(active) != count || len(log.Statements) != 3 {
			t.Fatalf("%d sources returned %d activities using %d queries: %v", count, len(active), len(log.Statements), err)
		}
		for i, work := range active {
			if work.WorkChatID != ids[i] || work.InputMessageID != "input-"+ids[i] || work.HasPublicReply != (i%2 == 1) {
				t.Fatalf("wrong publication state for work %s: %+v", ids[i], work)
			}
		}
		var plan []struct {
			// Detail 是 SQLite 返回的查询计划步骤。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN " + log.Statements[2]).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		indexed := false
		for _, step := range plan {
			if strings.HasPrefix(step.Detail, "SCAN main_conversation_messages") {
				t.Fatalf("%d sources scanned the publication table: %+v", count, plan)
			}
			indexed = indexed || strings.HasSuffix(step.Detail, "idx_main_conversation_messages_source_execution_id (source_execution_id=?)")
		}
		if !indexed {
			t.Fatalf("%d sources did not select by execution identity: %+v", count, plan)
		}
	}
}

func TestMainWorkRequestTargetsUsesBoundedRelatedQueries(t *testing.T) {
	db := newTestDB(t)
	const count = 100
	var teams []models.Channel
	var members []models.ChannelMember
	var conversations []models.MainConversation
	var sources []models.MainConversationWork
	for index := range count {
		teamID := fmt.Sprintf("team-%03d", index)
		conversationID := fmt.Sprintf("main-%03d", index)
		teams = append(teams, models.Channel{ID: teamID, Name: teamID, LeadWakerID: "lead"})
		members = append(members, models.ChannelMember{ChannelID: teamID, WakerID: "member"})
		conversations = append(conversations, models.MainConversation{ID: conversationID, SubjectType: "team", SubjectID: teamID})
		sources = append(sources, models.MainConversationWork{
			WorkChatID: fmt.Sprintf("source-%03d", index), ConversationID: conversationID, WakerID: "member",
		})
	}
	for _, rows := range []any{teams, members, conversations} {
		if err := db.CreateInBatches(rows, 50).Error; err != nil {
			t.Fatal(err)
		}
	}

	log := &repositoryQueryLog{Interface: logger.Discard}
	observed := db.Session(&gorm.Session{Logger: log})
	for _, size := range []int{1, count} {
		log.Statements = nil
		targets, err := mainWorkRequestTargets(observed, sources[:size])
		if err != nil || len(targets) != size || len(log.Statements) != 3 {
			t.Fatalf("%d interests returned %d targets using %d queries: %v", size, len(targets), len(log.Statements), err)
		}
		for index, source := range sources[:size] {
			target := targets[source.WorkChatID]
			if target.Title == nil || *target.Title != teams[index].Name || target.Owner.ID != source.ConversationID {
				t.Fatalf("incorrect team scope for %s: %+v", source.WorkChatID, target)
			}
		}
		for _, query := range log.Statements {
			var plan []struct {
				// Detail 是 SQLite 返回的查询计划步骤。
				Detail string
			}
			if err := db.Raw("EXPLAIN QUERY PLAN " + query).Scan(&plan).Error; err != nil {
				t.Fatal(err)
			}
			if len(plan) != 1 || !strings.HasPrefix(plan[0].Detail, "SEARCH ") {
				t.Fatalf("related query lacks an indexed search: %+v", plan)
			}
		}
	}
}
