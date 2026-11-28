package automatic_tasks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
)

func TestGlobalScheduleRejectsMissingOrUnusableWorkflowReportRecipient(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	_, base := workflowTask(t, h, `export const meta={sdk:'bot.workflow',title:'日报',phases:[{id:'work',title:'执行',detail:'流程',canExit:true}]};return 'done';`)
	base.WorkflowInputBindings = nil
	base.Trigger = constants.AutomaticTaskTriggerSchedule
	base.Schedule = models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "UTC", Hour: 12}
	for _, raw := range []string{
		`{"workflowReportRecipient":{}}`,
		`{"workflowReportRecipient":{"subjectType":"unsupported","subjectId":"other"}}`,
		`{"workflowReportRecipient":{"subjectType":"team","subjectId":"missing"}}`,
		`{"workflowReportRecipient":{"subjectType":"waker","subjectId":"missing"}}`,
	} {
		config := base
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.SaveConfiguration(ctx, "", "", 0, config, true); err == nil {
			t.Fatalf("global schedule accepted an unusable recipient: %s", raw)
		}
	}
}

func TestGlobalScheduledWorkflowFreezesRecipientAcrossChanges(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	_, config := workflowTask(t, h, `export const meta={sdk:'bot.workflow',title:'审核日报',phases:[{id:'work',title:'执行',detail:'流程',canExit:true}]};await askUser('批准？',{id:'approve',phase:'work',label:'批准',kind:'confirm'});return 'done';`)
	config.WorkflowInputBindings = nil
	config.Trigger = constants.AutomaticTaskTriggerSchedule
	config.Schedule = models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "UTC", Hour: 12}
	team := models.Channel{ID: "team", Name: "审核小组", LeadWakerID: h.wakerID}
	other := models.Waker{ID: "other", Name: "第二收件人"}
	if err := h.api.config.Wakers.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := h.api.config.Channels.Create(ctx, team, []models.ChannelMember{{ChannelID: team.ID, WakerID: other.ID}}); err != nil {
		t.Fatal(err)
	}
	teamMain, err := h.main.Ensure(ctx, "team", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	partnerMain, err := h.main.Ensure(ctx, "waker", h.wakerID)
	if err != nil {
		t.Fatal(err)
	}
	otherMain, err := h.main.Ensure(ctx, "waker", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := h.service.SaveConfiguration(ctx, "", "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.workflows.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.workflows.Close)
	submit := func(key string) schedule.RunView {
		t.Helper()
		task, err := h.api.config.Tasks.Get(ctx, saved.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := h.service.Admit(ctx, schedule.AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceSchedule, OccurrenceKey: key, RequestID: key, NextRunAt: task.NextRunAt})
		if err != nil {
			t.Fatal(err)
		}
		awaitScheduledWorkflow(t, h, run.WorkflowRunID, constants.WorkflowRunStatusWaiting, false)
		return run
	}
	saveRecipient := func(recipient *models.MainConversationRecipient) {
		t.Helper()
		task, err := h.api.config.Tasks.Get(ctx, saved.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		config = task.AutomaticTaskConfiguration
		config.WorkflowReportRecipient = recipient
		if _, err := h.service.SaveConfiguration(ctx, "", saved.TaskID, task.ConfigurationVersion, config, true); err != nil {
			t.Fatal(err)
		}
	}
	cancel := func(run schedule.RunView, expectedReport bool) models.WorkflowRun {
		t.Helper()
		if err := h.workflows.CancelRun(ctx, run.WorkflowRunID); err != nil {
			t.Fatal(err)
		}
		settled := awaitScheduledWorkflow(t, h, run.WorkflowRunID, constants.WorkflowRunStatusCancelled, expectedReport)
		if settled.ConclusionRequested != expectedReport || (settled.ConclusionRecipient != nil) != expectedReport {
			t.Fatalf("run %s has wrong frozen destination: %+v", run.WorkflowRunID, settled)
		}
		return settled
	}
	cancel(submit("old-disabled"), false)
	saveRecipient(&models.MainConversationRecipient{SubjectType: "team", SubjectID: team.ID})
	toTeam := submit("team")
	saveRecipient(&models.MainConversationRecipient{SubjectType: "waker", SubjectID: other.ID})
	if run := cancel(toTeam, true); run.ConclusionRecipient.SubjectID != team.ID || run.ConclusionDeliveryError != "" {
		t.Fatalf("team receipt lost after recipient was changed: %+v", run)
	}
	toPartner := submit("partner")
	saveRecipient(nil)
	if run := cancel(toPartner, true); run.ConclusionRecipient.SubjectID != other.ID || run.ConclusionDeliveryError != "" {
		t.Fatalf("partner receipt lost after reporting was disabled: %+v", run)
	}
	cancel(submit("disabled"), false)
	for _, expected := range []struct{ conversationID, runID string }{{teamMain.ID, toTeam.WorkflowRunID}, {otherMain.ID, toPartner.WorkflowRunID}, {partnerMain.ID, ""}} {
		rows, _, err := h.main.Messages(ctx, expected.conversationID, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if expected.runID == "" {
			if len(rows) != 0 {
				t.Fatalf("execution partner received reports: %+v", rows)
			}
			continue
		}
		if len(rows) != 1 || rows[0].WorkflowRun == nil || rows[0].WorkflowRun.RunID != expected.runID || !strings.Contains(rows[0].Content, "运行已停止") {
			t.Fatalf("incorrect main conclusion: %+v", rows)
		}
	}
	saveRecipient(&models.MainConversationRecipient{SubjectType: "waker", SubjectID: other.ID})
	deletedRecipient := submit("recipient-deleted")
	if err := h.api.config.Wakers.Delete(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	settled := cancel(deletedRecipient, true)
	if settled.ConclusionDeliveryError == "" {
		t.Fatalf("deleted recipient was recorded as delivered: %+v", settled)
	}
	if err := h.workflows.RetryConclusion(ctx, settled.ID); err != nil {
		t.Fatal(err)
	}
	replayed := awaitScheduledWorkflow(t, h, settled.ID, constants.WorkflowRunStatusCancelled, true)
	if replayed.ConclusionDeliveryError == "" || replayed.ConclusionRecipient.SubjectID != other.ID {
		t.Fatalf("retry changed the unavailable frozen address: %+v", replayed)
	}
	for _, expected := range []struct {
		id    string
		count int
	}{{teamMain.ID, 1}, {partnerMain.ID, 0}} {
		rows, _, err := h.main.Messages(ctx, expected.id, 0, 10)
		if err != nil || len(rows) != expected.count {
			t.Fatalf("retry delivered to a different subject: %+v %v", rows, err)
		}
	}
}

func awaitScheduledWorkflow(t *testing.T, h *testHarness, id string, status constants.WorkflowRunStatus, receipt bool) models.WorkflowRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := h.api.config.WorkflowRuns.GetRun(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == status && (!receipt || run.ConclusionFinishedAt != nil) {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s status=%s, receipt=%v, want status=%s, receipt=%v", id, run.Status, run.ConclusionFinishedAt != nil, status, receipt)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
