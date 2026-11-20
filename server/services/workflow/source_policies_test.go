package workflow

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const connectorPolicyWorkerSource = `export const meta={sdk:'bot.workflow',title:'复核',description:'复核',phases:[{id:'check',title:'检查',detail:'检查'}]};
return await worker('复核',{id:'review',phase:'check',label:'复核',resolve:{kind:'waker',wakerId:'one'},toolPolicy:TOOL_POLICY});`

func TestSaveWorkerSchemaWithoutExtraMeta(t *testing.T) {
	service, _ := workflowTestService(t)
	source := `export const meta={sdk:'bot.workflow',title:'分析',description:'报告',phases:[{id:'work',title:'分析',detail:'形成结论'}]};return await worker('分析',{id:'analysis',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'}});`
	definition := savedTestWorkflow(t, service, source)
	if definition.CurrentRevisionID == "" {
		t.Fatal("worker schema did not save")
	}
}

func TestSourcePoliciesRejectUnsupportedAllowlistsBeforeSave(t *testing.T) {
	for _, policy := range []string{
		`{connectors:{mode:'allowlist',connectorIds:[]}}`,
		`{connectors:{mode:'allowlist',connectorIds:['business']}}`,
		`{nativeTools:{mode:'allowlist',capabilityIds:[]}}`,
	} {
		t.Run(policy, func(t *testing.T) {
			ctx := t.Context()
			service, _ := workflowTestService(t)
			definition, err := fixtureDefinition(ctx, service.store, definitionFixture{Title: "复核"})
			if err != nil {
				t.Fatal(err)
			}
			source := strings.Replace(connectorPolicyWorkerSource, "TOOL_POLICY", policy, 1)
			if _, _, err := service.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: source, SDK: constants.WorkflowSDKIdentifier}); workflowstate.ErrorKey(err) != status_error.WorkflowCapabilityUnsupported.Key() {
				t.Fatalf("unexpected save for unsupported policy %s: %v", policy, err)
			}
			stored, err := service.store.GetDefinition(ctx, definition.ID)
			if err != nil || stored.CurrentRevisionID != "" {
				t.Fatalf("unsupported policy changed the workflow head: %+v %v", stored, err)
			}
		})
	}
}

func TestRunAdmissionRejectsPersistedWorkerConnectorAllowlist(t *testing.T) {
	ctx := t.Context()
	service, _ := workflowTestService(t)
	service.worker = &holdingWorker{}
	definition, err := fixtureDefinition(ctx, service.store, definitionFixture{Title: "复核"})
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(connectorPolicyWorkerSource, "TOOL_POLICY", `{connectors:{mode:'allowlist',connectorIds:[]}}`, 1)
	compiled, diagnostics, err := workflowdsl.Compile(source)
	if err != nil {
		t.Fatalf("compile stored source: %v %+v", err, diagnostics)
	}
	revision := models.WorkflowRevision{
		ID: util.New(), DefinitionID: definition.ID, Ordinal: 1, Source: source,
		SourceDigest: compiled.SourceDigest, ManifestDigest: compiled.ManifestDigest, IRDigest: compiled.IRDigest,
		SDK: constants.WorkflowSDKIdentifier, CompilerVersion: constants.WorkflowCompilerVersion, IRVersion: constants.WorkflowIRVersion,
		Manifest: compiled.Manifest, Bundle: compiled.Bundle,
	}
	if err := service.store.CreateRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RevisionID: revision.ID, RequestID: "run", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}}); workflowstate.ErrorKey(err) != status_error.WorkflowCapabilityUnsupported.Key() {
		t.Fatalf("admitted a revision with an unsupported connector policy: %v", err)
	}
	runs, err := service.runs.ListRuns(ctx, definition.ID, 0, "", 1)
	if err != nil || len(runs) != 0 {
		t.Fatalf("unsupported policy left a run record: %+v %v", runs, err)
	}
}
