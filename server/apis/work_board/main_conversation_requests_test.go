package work_board

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/workspace_attention"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestMainConversationRequestsReachAttentionAndResolveTheOriginalSource(t *testing.T) {
	db := boardDatabase(t)
	ctx := t.Context()
	member := models.Waker{ID: util.New(), Name: "工作伙伴"}
	lead := models.Waker{ID: util.New(), Name: "负责人"}
	team := models.Channel{ID: util.New(), Name: "协作团队", LeadWakerID: lead.ID}
	teamMember := models.ChannelMember{ChannelID: team.ID, WakerID: member.ID}
	for _, row := range []any{&member, &lead, &team, &teamMember} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := controllers.NewMainConversationController(db)
	wakerMain, err := store.Ensure(ctx, "waker", member.ID)
	if err != nil {
		t.Fatal(err)
	}
	teamMain, err := store.Ensure(ctx, "team", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	makeSource := func(ownerID string, approval bool) models.Chat {
		t.Helper()
		chatID := util.New()
		if ownerID != "" {
			work, err := store.ReserveWork(ctx, ownerID, member.ID, "requests")
			if err != nil {
				t.Fatal(err)
			}
			chatID = work.WorkChatID
		}
		chat := models.Chat{ID: chatID, WakerID: member.ID, HiddenFromWakerHistory: true, ActiveExecutionID: util.New(), Title: "内部工作"}
		if approval {
			chat.WaitingApproval = true
			chat.PendingApprovals = []models.PendingApproval{{ApprovalID: "request", Title: "确认工作", RequestedAt: 2}}
		} else {
			chat.WaitingElicitation = true
			chat.PendingElicitations = []models.PendingElicitation{{ElicitationID: "request", Message: "工作问题", RequestedAt: 1}}
		}
		if err := db.Create(&chat).Error; err != nil {
			t.Fatal(err)
		}
		return chat
	}
	wakerChat := makeSource(wakerMain.ID, true)
	teamChat := makeSource(teamMain.ID, false)
	makeSource("", false)

	readBell := func(want bool) {
		t.Helper()
		value, err := callBoard(t, &workspace_attention.GetWorkspaceAttention{}, "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		bell := value.(models.WorkspaceAttentionSnapshot)
		if bell.HasUnread || bell.HasAction != want {
			t.Fatalf("request did not reach the action-only indicator: %+v", bell)
		}
	}
	readBell(true)
	value, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := value.(WorkBoardSummary)
	if summary.PendingRequestCount != 2 || summary.WaitingWorkCount != 2 || len(summary.ActionPreview) != 2 {
		t.Fatalf("main requests missing or a hidden unrelated request escaped: %+v", summary)
	}
	for _, test := range []struct {
		// OwnerID 是伙伴或团队的公开主会话。
		OwnerID string
		// ExecutorID 是待答所属主体。
		ExecutorID string
		// ChannelID 是团队标识，伙伴的为空。
		ChannelID string
		// Source 是看板按归属使用的来源筛选。
		Source string
		// Title 是公开主体名称。
		Title string
		// Kind 是请求类型。
		Kind string
		// Chat 是实际产生请求的原工作来源。
		Chat models.Chat
	}{
		{wakerMain.ID, member.ID, "", "chat", member.Name, "approval", wakerChat},
		{teamMain.ID, team.ID, team.ID, "team", team.Name, "input", teamChat},
	} {
		value, err := callBoard(t, &ListWorkBoard{}, "?mode=action&limit=1&source="+test.Source+"&executorId="+test.ExecutorID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		page := value.(WorkBoardPage)
		if page.TotalSize != 1 || len(page.Attention) != 1 {
			t.Fatalf("subject filter %s did not return its one request: %+v", test.ExecutorID, page)
		}
		request := page.Attention[0]
		if request.OwnerKind != constants.WorkOwnerKindMainConversation || request.OwnerID != test.OwnerID || request.ExecutorID != test.ExecutorID ||
			request.Source != test.Source || request.WorkTitle != test.Title || request.ChannelID != test.ChannelID || request.WakerID != member.ID ||
			request.ChatID != test.Chat.ID || request.ExecutionID != test.Chat.ActiveExecutionID || request.Kind != test.Kind || request.RequestID != "request" {
			t.Fatalf("request lost its public owner or exact source: %+v", request)
		}
	}
	actors, _, err := global.WorkspaceAttention.ExecutorSummaries(ctx, constants.LocalReaderID, []string{member.ID, team.ID, lead.ID})
	if err != nil || len(actors) != 3 {
		t.Fatalf("summary: %+v %v", actors, err)
	}
	for i, ownerID := range []string{wakerMain.ID, teamMain.ID} {
		row := actors[i]
		if row.PendingRequestCount != 1 || row.WaitingWorkCount != 1 || len(row.Chats) != 0 || len(row.Requests) != 1 || row.Requests[0].OwnerID != ownerID {
			t.Fatalf("request omitted or counted twice in subject summary: %+v", row)
		}
	}
	if actors[2].PendingRequestCount != 0 {
		t.Fatalf("team request leaked to its lead's sidebar: %+v", actors[2])
	}

	query := url.Values{"operation": {"request_opened"}, "chatId": {teamChat.ID}, "requestId": {"request"}, "requestKind": {"input"}}
	resolve := func(kind, owner, executionID string, valid bool) {
		t.Helper()
		query.Set("workKind", kind)
		query.Set("workId", owner)
		query.Set("executionId", executionID)
		value, err := callBoard(t, &GetWorkBoardLiveTarget{}, "?"+query.Encode(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		target := value.(WorkBoardLiveTarget)
		if target.Valid != valid || valid && (target.Work == nil || target.Work.Kind != constants.WorkOwnerKindMainConversation ||
			target.Work.ID != teamMain.ID || target.ChatID != teamChat.ID || target.ChannelID != team.ID || target.WakerID != member.ID) {
			t.Fatalf("unexpected exact request target: %+v", target)
		}
	}
	resolve("chat", teamChat.ID, teamChat.ActiveExecutionID, true)
	resolve("main_conversation", teamMain.ID, teamChat.ActiveExecutionID, true)
	resolve("main_conversation", wakerMain.ID, teamChat.ActiveExecutionID, false)
	resolve("chat", teamChat.ID, util.New(), false)

	board := global.EventHub.Subscribe("work-board", events.Resource{Kind: "global"})
	defer board.Close()
	mainEvents := global.EventHub.Subscribe(constants.MainConversationEventTopic, events.Resource{Kind: "team", ID: team.ID})
	defer mainEvents.Close()
	otherMainEvents := global.EventHub.Subscribe(constants.MainConversationEventTopic, events.Resource{Kind: "waker", ID: member.ID})
	defer otherMainEvents.Close()
	channel := controllers.NewChannelController(db)
	changeTeam := func(members []models.ChannelMember, rollback bool) {
		t.Helper()
		err := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
			if err := channel.Update(tx, team, members); err != nil {
				return err
			}
			if len(board.Events) != 0 || len(mainEvents.Events) != 0 {
				t.Fatal("team eligibility was published before commit")
			}
			if rollback {
				return status_error.Conflict
			}
			return nil
		})
		if rollback && !errors.Is(err, status_error.Conflict) || !rollback && err != nil {
			t.Fatalf("team settings transaction: %v", err)
		}
	}
	leadMember := models.ChannelMember{ChannelID: team.ID, WakerID: lead.ID}
	team.Description = "更新团队说明"
	changeTeam([]models.ChannelMember{teamMember, leadMember}, false)
	changeTeam([]models.ChannelMember{leadMember}, true)
	if len(board.Events) != 0 || len(mainEvents.Events) != 0 {
		t.Fatal("unchanged request scope or a rollback refreshed the work board or main conversation")
	}
	resolve("chat", teamChat.ID, teamChat.ActiveExecutionID, true)
	changeTeam([]models.ChannelMember{leadMember}, false)
	select {
	case changed := <-board.Events:
		var payload map[string]string
		if err := json.Unmarshal(changed.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if changed.Transient || changed.Type != "work-board.changed" || changed.Resource != (events.Resource{Kind: "main_conversation", ID: teamMain.ID}) || payload["reason"] != "requests-changed" {
			t.Fatalf("team eligibility refreshed the wrong scope: %+v", changed)
		}
	default:
		t.Fatal("team eligibility did not refresh the work board after commit")
	}
	if len(board.Events) != 0 {
		t.Fatal("one team change sent multiple work-board refreshes")
	}
	resolve("chat", teamChat.ID, teamChat.ActiveExecutionID, false)
	active, err := store.ActiveWork(ctx, teamMain.ID)
	if err != nil || len(active) != 0 {
		t.Errorf("revoked source still appears as actionable main work: %+v (%v)", active, err)
	}
	if len(mainEvents.Events) != 1 || len(otherMainEvents.Events) != 0 {
		t.Fatalf("eligibility change did not invalidate only the original main conversation: team=%d unrelated=%d", len(mainEvents.Events), len(otherMainEvents.Events))
	}
	changed := <-mainEvents.Events
	if changed.Transient || changed.Type != "main-conversations.changed" || changed.Resource != (events.Resource{Kind: "team", ID: team.ID}) {
		t.Fatalf("main conversation eligibility refreshed the wrong scope: %+v", changed)
	}
	if err := db.Model(&wakerChat).Update("active_execution_id", "").Error; err != nil {
		t.Fatal(err)
	}
	readBell(false)
}
