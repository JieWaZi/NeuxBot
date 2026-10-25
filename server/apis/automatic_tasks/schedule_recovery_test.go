package automatic_tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func persistPending(t *testing.T, h *testHarness, taskID, key string, source constants.AutomaticRunSource) models.AutomaticRun {
	t.Helper()
	ctx := context.Background()
	task, _ := h.api.config.Tasks.Get(ctx, taskID)
	if _, err := h.service.Get(ctx, h.wakerID, taskID); err != nil {
		t.Fatal(err)
	}
	task, _ = h.api.config.Tasks.Get(ctx, taskID)
	hash := sha256.Sum256(nil)
	run := models.AutomaticRun{ID: util.New(), TaskID: taskID, WakerID: h.wakerID, OccurrenceKey: key, RequestHash: hex.EncodeToString(hash[:]), Source: source, Configuration: task.AutomaticTaskConfiguration,
		ConfigurationRevision: task.ConfigurationVersion, Admission: constants.AutomaticRunAdmissionPending, ExecutionID: util.New()}
	err := h.api.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		claimed, err := h.api.config.Tasks.ClaimRun(tx, taskID, task.Revision, run.ID, task.NextRunAt)
		if err != nil {
			return err
		}
		if !claimed {
			return status_error.AutomaticTaskBusy
		}
		return h.api.config.Tasks.CreateRun(tx, run)
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAutomaticRecoveryRequiresExplicitSameKeyBeforeChat(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	saved := h.save(t, constants.AutomaticTaskTriggerAPI)
	pending := persistPending(t, h, saved.TaskID, "api:1:recover", constants.AutomaticRunSourceAPI)
	if err := h.restart(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.boundary.intents) != 0 {
		t.Fatal("restart dispatched pending API request")
	}
	task, _ := h.api.config.Tasks.Get(ctx, saved.TaskID)
	if task.ActiveRunID != "" {
		t.Fatal("pending recovery held runtime slot without Chat")
	}
	receipt, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "recover", "")
	if err != nil || receipt.ID != pending.ID || len(h.boundary.intents) != 1 {
		t.Fatalf("explicit recovery: %+v %v", receipt, err)
	}
	h.boundary.finish(receipt.ChatID, constants.ExecutionStatusCompleted, false)
	pending2 := persistPending(t, h, saved.TaskID, "api:1:revoked", constants.AutomaticRunSourceAPI)
	if err := h.restart(ctx); err != nil {
		t.Fatal(err)
	}
	task, _ = h.api.config.Tasks.Get(ctx, saved.TaskID)
	task.Instruction = "已撤销旧指令"
	if _, err := h.service.SaveConfiguration(ctx, h.wakerID, saved.TaskID, task.ConfigurationVersion, task.AutomaticTaskConfiguration, true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "revoked", ""); !errors.Is(err, status_error.AutomaticTaskAdmissionRevoked) {
		t.Fatal("revoked snapshot executed")
	}
	stored, _ := h.api.config.Tasks.GetRun(ctx, pending2.ID)
	if stored.Admission != constants.AutomaticRunAdmissionInterrupted || stored.ChatID != "" || len(h.boundary.intents) != 1 {
		t.Fatal("revoked pending request left invalid facts")
	}
}

func TestAutomaticRecoveryReconcilesDurableChatBeforeLinkAndInterruptsTimer(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	saved := h.save(t, constants.AutomaticTaskTriggerAPI)
	pending := persistPending(t, h, saved.TaskID, "api:1:durable", constants.AutomaticRunSourceAPI)
	accepted, err := h.boundary.Start(ctx, execution.WorkIntent{ClientRequestID: pending.ID, ExecutionID: pending.ExecutionID, Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: h.wakerID, HiddenFromWakerHistory: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.restart(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.api.config.Tasks.GetRun(ctx, pending.ID)
	if stored.Admission != constants.AutomaticRunAdmissionAccepted || stored.ChatID != accepted.Chat.ID {
		t.Fatal("durable authority was not linked")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "durable", ""); err != nil || len(h.boundary.intents) != 1 {
		t.Fatal("durable request dispatched twice")
	}
	timer := h.save(t, constants.AutomaticTaskTriggerSchedule)
	opportunity := persistPending(t, h, timer.TaskID, "schedule:1:past", constants.AutomaticRunSourceSchedule)
	if err := h.restart(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _ = h.api.config.Tasks.GetRun(ctx, opportunity.ID)
	if stored.Admission != constants.AutomaticRunAdmissionInterrupted || stored.ChatID != "" || len(h.boundary.intents) != 1 {
		t.Fatal("pending timer was replayed after restart")
	}
}

func TestAutomaticOnceManualDoesNotConsumePlanAndDependencyFailureIsRecorded(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	config := h.config(constants.AutomaticTaskTriggerSchedule)
	config.Schedule = models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindOnce, Timezone: "UTC", OnceAt: h.api.config.Now().Add(time.Minute).Format(constants.AutomaticTaskLocalTimeLayout)}
	saved, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := h.api.config.Tasks.Get(ctx, saved.TaskID)
	run, err := h.api.TriggerManual(ctx, h.wakerID, saved.TaskID, "manual-once")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := h.api.config.Tasks.Get(ctx, saved.TaskID)
	if after.NextRunAt == nil || *after.NextRunAt != *before.NextRunAt {
		t.Fatal("manual run consumed one-shot plan")
	}
	h.boundary.finish(run.ChatID, constants.ExecutionStatusCompleted, false)
	h.boundary.invalid = status_error.RuntimeUnavailable
	h.advanceContinuous(t, 60)
	if err := h.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	history, total, err := h.api.Runs(ctx, h.wakerID, saved.TaskID, 20, 0)
	if err != nil || total != 2 || history[0].Admission != constants.AutomaticRunAdmissionFailed || history[0].ChatID != "" {
		t.Fatalf("unavailable runtime did not leave failed opportunity: total=%d error=%v history=%+v", total, err, history)
	}
	if err := h.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	_, total, _ = h.api.Runs(ctx, h.wakerID, saved.TaskID, 20, 0)
	if total != 2 || len(h.boundary.intents) != 1 {
		t.Fatal("failed accepted opportunity was automatically retried")
	}
}
