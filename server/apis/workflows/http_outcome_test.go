package workflows

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

const unknownHTTPOutcomeSource = `export const meta={sdk:'ally.workflow/1',title:'HTTP回执',description:'外部效果对账',actions:[{id:'post',label:'提交',description:'非幂等写入',type:'http',risk:'write',allowedOrigins:['http://workflow.example'],methods:['POST']}],phases:[{id:'write',title:'写入',detail:'写入',canExit:true}]};return await action('post',{id:'post-node',label:'提交',phase:'write',method:'POST',url:'http://workflow.example/write'});`

func TestUnknownHTTPOutcomeFailsAndBlocksBlindRetry(t *testing.T) {
	for _, mode := range []string{"disconnect", "truncated", "cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _ := workflowTestService(t)
			definition := savedTestWorkflow(t, s, unknownHTTPOutcomeSource)
			run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "first", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 1)
			state, err := s.Runs.LoadRunState(ctx, run.ID)
			if err != nil || len(state.Effects) != 1 {
				t.Fatalf("prepare: %+v %v", state, err)
			}
			effect := state.Effects[0]
			if mode == "cancel" {
				if err := s.CancelRun(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			// 夹具保存真实网络测试已经证明的未知回执；本测试只核验用户命令和运行恢复。
			err = s.State.MutateEffect(ctx, run.ID, effect.ID, func(_ context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
				failure := map[string]any{"code": "EFFECT_OUTCOME_UNKNOWN", "cause": mode}
				if mode == "cancel" {
					failure[constants.WorkflowFailureStopRequestedField] = true
				}
				value := workflowdsl.JSONValue(failure)
				current.Status = constants.WorkflowStepStatusUnknown
				current.Failure = &value
				state.Run.Status = constants.WorkflowRunStatusRunning
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			paused := runTicks(t, s, run.ID, 2)
			if paused.Status != constants.WorkflowRunStatusFailed {
				t.Fatalf("unreceipted action was treated as known failure: %+v", paused)
			}
			deps := workflowAPIDependencies(s)
			if _, err := retryRun(ctx, deps, RetryRunCommand{RunID: run.ID, RequestID: "unsafe", ExpectedVersion: paused.Version}); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
				t.Fatalf("unreconciled action could be retried: %v", err)
			}
			if err := s.CancelRun(ctx, run.ID); err != nil {
				t.Fatalf("cancel erased uncertain external outcome: %v", err)
			}
			final := runTicks(t, s, run.ID, 3)
			state, err = s.Runs.LoadRunState(ctx, run.ID)
			if err != nil || final.Status != constants.WorkflowRunStatusFailed || final.Failure == nil || state.Effects[0].Status != constants.WorkflowStepStatusUnknown || state.Instances[0].Status != constants.WorkflowStepStatusFailed {
				t.Fatalf("uncertain operation was resumed or lost failure evidence: %+v %v", state, err)
			}

		})
	}
}

func TestUnknownEffectRecoversAsFailureWithOriginalEvidence(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, unknownHTTPOutcomeSource)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "recover", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, err := s.Runs.LoadRunState(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.Run.Status = constants.WorkflowRunStatusRunning
	state.Run.DeadlineAt = 1
	state.Effects[0].Status = constants.WorkflowStepStatusUnknown
	value := workflowdsl.JSONValue(map[string]any{"code": "EFFECT_OUTCOME_UNKNOWN", "cause": "interrupted-response"})
	state.Effects[0].Failure = &value
	// 保存中断时的效果事实，核验启动恢复能够形成失败并保留原证据。
	if err := s.Runs.CommitRunState(ctx, state, state.Run.Version, state.Run.UpdatedAt+1); err != nil {
		t.Fatal(err)
	}
	runnable, err := s.Runs.ListRunnable(ctx, 50)
	if err != nil || len(runnable) != 1 || runnable[0].ID != run.ID {
		t.Fatalf("interrupted run excluded from recovery: %+v %v", runnable, err)
	}
	final := runTicks(t, s, run.ID, 3)
	state, err = s.Runs.LoadRunState(ctx, run.ID)
	if err != nil || final.Status != constants.WorkflowRunStatusFailed || final.CompletedAt == nil || state.Instances[0].Status != constants.WorkflowStepStatusFailed || state.Effects[0].Failure.Data.(map[string]any)["cause"] != "interrupted-response" {
		t.Fatalf("recovered failure did not converge or lost evidence: %+v %v", state, err)
	}
}
