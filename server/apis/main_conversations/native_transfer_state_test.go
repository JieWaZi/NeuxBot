package main_conversations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

func TestWorkTicketTransferRejectsUnavailableLocalSelectionBeforeSealing(t *testing.T) {
	configureNativeMainConversation(t)
	initial := startTransferFixture(t)
	ctx := t.Context()
	for _, target := range []models.Waker{
		{ID: "missing-runtime", Name: "Missing runtime", DefaultModel: &models.WakerDefaultModel{RuntimeID: "not-installed"}},
		{ID: "missing-account", Name: "Missing account", DefaultModel: &models.WakerDefaultModel{RuntimeID: constants.RuntimeIDCodex, AccountID: "removed-account"}},
	} {
		if err := global.WakerControllerManager.Create(ctx, target); err != nil {
			t.Fatal(err)
		}
		initial.OwnerID, initial.RequestID = target.ID, target.ID
		if _, err := global.TransferWorkTicket(ctx, initial); err == nil {
			t.Errorf("unavailable selection for %s sealed the original attempt", target.ID)
		}
		current, found, err := global.WorkTicketService.ForMessage(ctx, initial.Address, initial.TicketID)
		if err != nil || !found || current.OwnerID != "native-main-partner" || current.Generation != initial.ExpectedGeneration {
			t.Fatalf("rejected transfer displaced the original owner: %+v %v", current, err)
		}
	}
}

func TestWorkTicketTransferKeepsFrozenMemberOrBlocksRevokedMember(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		name := "retained"
		if revoked {
			name = "revoked"
		}
		t.Run(name, func(t *testing.T) {
			configureNativeMainConversation(t)
			initial := startTransferFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			for _, id := range []string{"frozen-member", "new-lead", "backup-member"} {
				if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
					t.Fatal(err)
				}
			}
			team := models.Channel{ID: "handoff-team", Name: "Handoff team", LeadWakerID: "frozen-member"}
			frozen := models.ChannelMember{ChannelID: team.ID, WakerID: "frozen-member", RuntimeID: constants.RuntimeIDCodex}
			members := []models.ChannelMember{frozen}
			if err := global.ChannelControllerManager.Create(ctx, team, members); err != nil {
				t.Fatal(err)
			}
			initial.OwnerKind, initial.OwnerID = "team", team.ID
			receipt, err := global.TransferWorkTicket(ctx, initial)
			if err != nil {
				t.Fatal(err)
			}
			team.LeadWakerID = "new-lead"
			members = append(members, models.ChannelMember{ChannelID: team.ID, WakerID: team.LeadWakerID, RuntimeID: constants.RuntimeIDCodex})
			if revoked {
				members = members[1:]
			}
			if err := global.ChannelControllerManager.Update(ctx, team, members); err != nil {
				t.Fatal(err)
			}
			workerCtx, stopWorker := context.WithCancel(ctx)
			workerDone := make(chan struct{})
			wake := make(chan struct{}, 1)
			go func() {
				defer close(workerDone)
				global.RunWorkTicketCollaboration(workerCtx, wake, global.ExecutionService)
			}()
			defer func() { stopWorker(); <-workerDone }()
			wait := func(desired constants.WorkTicketStatus) models.WorkTicket {
				t.Helper()
				tick := time.NewTicker(10 * time.Millisecond)
				defer tick.Stop()
				for {
					ticket, found, err := global.WorkTicketService.ForMessage(ctx, initial.Address, initial.TicketID)
					if err != nil {
						t.Fatal(err)
					}
					if found && ticket.Status == desired {
						return ticket
					}
					select {
					case <-tick.C:
					case <-ctx.Done():
						t.Fatalf("wanted %s after team change, got %+v", desired, ticket)
					}
				}
			}
			if !revoked {
				if ticket := wait(constants.WorkTicketStatusCompleted); ticket.OwnerWakerID != frozen.WakerID || ticket.CurrentExecutionID != receipt.ExecutionID {
					t.Fatalf("handoff silently chose the new lead: %+v", ticket)
				}
				return
			}
			blocked := wait(constants.WorkTicketStatusBlocked)
			if blocked.OwnerWakerID != frozen.WakerID || blocked.CurrentExecutionID != "" || blocked.PendingExecutionID != "" || strings.TrimSpace(blocked.Summary) == "" {
				t.Fatalf("revocation was not recoverable from the original ticket: %+v", blocked)
			}
			if replay, err := global.TransferWorkTicket(ctx, initial); err != nil || replay != receipt {
				t.Fatalf("blocked receipt was not replayable: %+v %v", replay, err)
			}
			wrong := initial
			wrong.Address.ID = "other-chat"
			wrong.RequestID = "forbidden"
			wrong.ExpectedGeneration = blocked.Generation
			wrong.OwnerKind, wrong.OwnerID = "waker", "backup-member"
			if err := global.ChatControllerManager.Create(ctx, models.Chat{ID: wrong.Address.ID, WakerID: "native-main-partner"}); err != nil {
				t.Fatal(err)
			}
			if _, err := global.TransferWorkTicket(ctx, wrong); !util.HasStatusCode(err, status_error.WorkTicketNotFound) {
				t.Fatalf("another origin could recover revoked handoff: %v", err)
			}
			initial.RequestID, initial.ExpectedGeneration = "recover-transfer", blocked.Generation
			// The same team can receive an explicit new transfer under its current lead.
			recovery, err := global.TransferWorkTicket(ctx, initial)
			if err != nil || recovery.Generation != blocked.Generation+1 || recovery.ExecutionID == receipt.ExecutionID {
				t.Fatalf("original user could not redirect blocked transfer: %+v %v", recovery, err)
			}
			wake <- struct{}{}
			if ticket := wait(constants.WorkTicketStatusCompleted); ticket.OwnerWakerID != team.LeadWakerID || ticket.CurrentExecutionID != recovery.ExecutionID {
				t.Fatalf("new transfer did not settle under the selected owner: %+v", ticket)
			}
		})
	}
}

func startTransferFixture(t *testing.T) workticket.TransferInput {
	t.Helper()
	accepted, err := global.ExecutionService.Start(t.Context(), execution.WorkIntent{
		ClientRequestID: "transfer-state-original", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: "native-main-partner"}},
		Input: execution.WorkInput{Actor: "user", Content: "Inspect the harmless fixture."},
		Options: execution.WorkOptions{EntryPoint: execution.WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto}, Ticket: execution.WorkTicketInput{Start: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return workticket.TransferInput{Address: workticket.Address{Kind: constants.WorkTicketOriginChat, ID: accepted.Chat.ID},
		TicketID: accepted.Execution.UserMessageID, ExpectedGeneration: 1, RequestID: "transfer-state-handoff", OwnerKind: "waker", Content: "fixture explicit completed"}
}
