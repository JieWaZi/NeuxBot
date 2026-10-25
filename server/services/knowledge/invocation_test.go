package knowledge

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	keyrules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/neux-knowledge/core"
	"testing"
)

func TestIntegrationAutomaticTaskKnowledgeProvenance(t *testing.T) {
	s, lib, _, db := newServiceFixtureWithDB(t)
	ctx := context.Background()
	tasks := controllers.NewAutomaticTaskController(db)
	keys := controllers.NewIntegrationKeyController(db)
	_, _, _ = keys.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "key", Digest: "fixture", KeyID: "key", Action: "create"})
	key, err := keys.CompleteMutation(ctx, "key", "fixture", models.IntegrationKey{ID: "key", Name: "调用方", KeyHash: keyrules.HashKey("fixture"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceIDs: []string{"task"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	authority := integrationservice.New(integrationservice.Config{Keys: keys, Tasks: tasks})
	identity, _ := authority.Authenticate(ctx, "fixture")
	raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("正文")})
	if err != nil {
		t.Fatal(err)
	}
	grant := models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: lib.ID, Operation: "organize", SourceVersionIDs: []string{raw.ResourceID}}
	task := models.AutomaticTask{ID: "task", WakerID: "waker", Revision: 1, ConfigurationVersion: 1, AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{Name: "任务", Enabled: true, Trigger: constants.AutomaticTaskTriggerAPI, Knowledge: &grant}}
	if err = tasks.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	run := models.AutomaticRun{ID: "run", TaskID: "task", WakerID: "waker", Source: constants.AutomaticRunSourceAPI, OccurrenceKey: "original", Configuration: task.AutomaticTaskConfiguration, IntegrationInvocation: &models.IntegrationInvocation{Identity: identity, TaskID: "task", TaskConfigurationVersion: 1}}
	if err = tasks.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	s.config.AutomaticRun = tasks.GetRun
	s.config.InvocationGuard = authority.AcquireInvocationGuard
	intent := execution.WorkIntent{AutomaticRunID: run.ID, ClientRequestID: run.ID, Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: "waker"}}, Input: execution.WorkInput{Content: "整理正文"}, Options: execution.WorkOptions{RuntimeID: "codex"}}
	bound, err := s.BindIntent(ctx, intent, grant)
	if err != nil {
		t.Fatal(err)
	}
	result, err := bound.Options.ToolCapabilities[0].Bindings[0].Call(ctx, agentcapability.Request{RequestID: "knowledge"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := result.Data.(map[string]any)["receipt"].(*core.Receipt)
	job, err := s.config.Engine.GetJob(ctx, receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := s.config.References.GetTaskConfiguration(ctx, job.RequestID)
	if err != nil || config.RemoteSubject != "key" {
		t.Fatal(config, err)
	}
	var recovery struct {
		Origin         string `json:"origin"`
		AutomaticRunID string `json:"automaticRunId"`
	}
	if json.Unmarshal(config.RemoteAuthorization, &recovery) != nil || recovery.Origin != "automatic_task" || recovery.AutomaticRunID != run.ID {
		t.Fatal(string(config.RemoteAuthorization))
	}
	key.Enabled = false
	if err = authority.WithAuthorizationWrite(ctx, func(ctx context.Context) error {
		_, err := keys.UpdateAuthorization(ctx, key, key.Revision)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if release, err := s.publishGuard(config)(ctx); err == nil {
		release()
		t.Fatal("knowledge detached itself from original revoked task caller")
	}
}
