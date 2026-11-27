package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
)

func TestDeleteChannelWithUnmaterializedMainWork(t *testing.T) {
	fixture := newDeletionFixture(t)
	ctx := context.Background()
	team := models.Channel{ID: "team", Name: "Team", LeadWakerID: "lead"}
	if err := fixture.channels.Create(ctx, team, []models.ChannelMember{{ChannelID: team.ID, WakerID: team.LeadWakerID}}); err != nil {
		t.Fatal(err)
	}
	chats, err := chatservice.New(chatservice.Config{
		StorageDirectory: t.TempDir(), Projects: controllers.NewProjectController(fixture.db),
		QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(fixture.db),
			DeletionIntents: controllers.NewChatDeletionIntentController(fixture.db), Transactions: fixture.service.transactions},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.chats = chats
	store := controllers.NewMainConversationController(fixture.db)
	main := mainconversation.New(store, fixture.service.transactions, controllers.NewWakerController(fixture.db), fixture.channels)
	work, err := main.ReserveWork(ctx, "team", team.ID, team.LeadWakerID, "project:known")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chats.GetChatSummary(ctx, work.WorkChatID); err == nil {
		t.Fatal("work Chat should not exist before its first successful start")
	}
	if err := fixture.service.DeleteChannel(ctx, team.ID); err != nil {
		t.Fatalf("delete team with empty reservation: %v", err)
	}
	if _, exists, err := store.FindWork(ctx, work.WorkChatID); err != nil || exists {
		t.Fatalf("reservation remained: %t %v", exists, err)
	}
	if _, err := fixture.channels.Get(ctx, team.ID); err == nil {
		t.Fatal("deleted team remained")
	}
}

func TestDeleteWakerWithIdleTeamMainWork(t *testing.T) {
	fixture := newDeletionFixture(t)
	ctx := t.Context()
	team := models.Channel{ID: "team", Name: "Team", LeadWakerID: "lead"}
	wakers := controllers.NewWakerController(fixture.db)
	for _, id := range []string{"lead", "member"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.channels.Create(ctx, team, []models.ChannelMember{
		{ChannelID: team.ID, WakerID: "lead"},
		{ChannelID: team.ID, WakerID: "member", Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	chats, err := chatservice.New(chatservice.Config{
		StorageDirectory: root, Projects: controllers.NewProjectController(fixture.db),
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats:           controllers.NewChatController(fixture.db),
			DeletionIntents: controllers.NewChatDeletionIntentController(fixture.db),
			Transactions:    fixture.service.transactions,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chats.Close)
	fixture.service.chats, fixture.service.wakers = chats, wakers
	store := controllers.NewMainConversationController(fixture.db)
	main := mainconversation.New(store, fixture.service.transactions, wakers, fixture.channels)
	work, err := main.ReserveWork(ctx, "team", team.ID, team.LeadWakerID, "managed")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := chatstorage.NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := layout.AgentWorkspace(team.LeadWakerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureAgentWorkspace(dir, team.LeadWakerID); err != nil {
		t.Fatal(err)
	}
	_, err = chats.CreateDormantChats(ctx, []chatservice.DormantChat{{Chat: models.Chat{
		ID: work.WorkChatID, WakerID: team.LeadWakerID, WorkspaceDirectory: dir,
		AdditionalWorkingDirectories: []string{dir}, HiddenFromWakerHistory: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	deleteWaker := func(fail bool) error {
		return fixture.service.DetachWaker(ctx, team.LeadWakerID, func(ctx context.Context, relatedChatIDs []string, detach func(context.Context) error) error {
			return chats.DeleteWakerChats(ctx, team.LeadWakerID, relatedChatIDs,
				func(ids []string) (func() error, error) {
					if len(ids) != 1 || ids[0] != work.WorkChatID {
						t.Fatalf("expected the original work Chat in the partner deletion: %v", ids)
					}
					return nil, nil
				},
				func(tx context.Context) error {
					if err := detach(tx); err != nil {
						return err
					}
					if fail {
						return status_error.Conflict
					}
					return wakers.Delete(tx, team.LeadWakerID)
				},
			)
		})
	}
	if err := fixture.db.Model(&models.Chat{}).Where("id = ?", work.WorkChatID).Update("active_execution_id", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := deleteWaker(false); !errors.Is(err, status_error.ChannelHasActiveWork) {
		t.Fatalf("an active main work Chat did not block implicit team deletion: %v", err)
	}
	if err := fixture.db.Model(&models.Chat{}).Where("id = ?", work.WorkChatID).Update("active_execution_id", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := deleteWaker(true); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("finalize error = %v", err)
	}
	if _, err := fixture.channels.Get(ctx, team.ID); err != nil {
		t.Fatalf("failed transaction removed a team: %v", err)
	}
	if _, err := chats.GetChatSummary(ctx, work.WorkChatID); err != nil {
		t.Fatalf("failed transaction removed the work Chat: %v", err)
	}
	if _, found, err := store.FindWork(ctx, work.WorkChatID); err != nil || !found {
		t.Fatalf("failed transaction removed the source binding: %t %v", found, err)
	}
	if err := deleteWaker(false); err != nil {
		t.Fatalf("idle lead deletion failed: %v", err)
	}
	if _, err := chats.GetChatSummary(ctx, work.WorkChatID); err == nil {
		t.Fatal("work Chat remained after its owner was deleted")
	}
	if _, err := fixture.channels.Get(ctx, team.ID); err == nil {
		t.Fatal("team remained after becoming too small")
	}
}
