package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// corruptedRevisionStore 模拟存储损坏或未来程序版本，其他数据仍由实际 SQLite 提供。
type corruptedRevisionStore struct {
	// WorkflowController 保留真实定义、收据和事务行为。
	controllers.WorkflowController
	// revision 是从真实数据库读取后损坏的不可变版本。
	revision models.WorkflowRevision
}

func TestImpossibleSavedInstanceBudgetBlocksAdmissionBeforeAnyWork(t *testing.T) {
	service, store := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow/1',title:'预算接纳',description:'旧保存记录同样检查',limits:{maxInstances:2},phases:[{id:'work',title:'工作',detail:'两步',canExit:true}]};await askUser('第一步',{id:'first',label:'第一步',phase:'work'});await askUser('第二步',{id:'second',label:'第二步',phase:'work'});return true;`
	definition := savedTestWorkflow(t, service, source)
	ctx := context.Background()
	revision, err := store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟编译器尚未检查最少实例数时保存的完整、自洽版本，而非损坏摘要。
	revision.Source = strings.Replace(source, "maxInstances:2", "maxInstances:1", 1)
	revision.SourceDigest = workflowdsl.Digest([]byte(revision.Source))
	revision.Manifest.Meta.Limits.MaxInstances = 1
	manifest, err := json.Marshal(revision.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	revision.ManifestDigest = workflowdsl.Digest(manifest)
	service.store = corruptedRevisionStore{WorkflowController: store, revision: revision}
	_, err = service.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
	var detail *statuserror.StatusErr
	if !errors.As(err, &detail) || detail.Key() != status_error.WorkflowBudgetExceeded.Key() || detail.Fields["budget"] != constants.WorkflowInstanceBudget || detail.Fields["reason"] == "" {
		t.Fatalf("impossible budget did not return its admission diagnostic: %v", err)
	}
	runs, err := service.runs.ListRuns(ctx, definition.ID, 0, "", 50)
	if err != nil || len(runs) != 0 {
		t.Fatalf("budget rejection left a run or external work behind: %d %v", len(runs), err)
	}
}

func (s corruptedRevisionStore) GetRevision(context.Context, string, string) (models.WorkflowRevision, error) {
	return s.revision, nil
}

func TestUnsupportedOrCorruptedRevisionBlocksAdmissionAndStopsActiveRun(t *testing.T) {
	for _, mode := range []string{"source", "manifest", "ir_version"} {
		t.Run(mode, func(t *testing.T) {
			service, store := workflowTestService(t)
			definition := savedTestWorkflow(t, service, humanSource)
			ctx := context.Background()
			run, err := service.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "before"})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, service, run.ID, 2)
			revision, err := store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "source":
				revision.Source += "\n// corrupt"
			case "manifest":
				revision.Manifest.Meta.Title = "changed"
			case "ir_version":
				revision.IRVersion++
			}
			service.store = corruptedRevisionStore{WorkflowController: store, revision: revision}
			if _, err := service.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "after"}); err == nil {
				t.Fatal("bad revision admitted")
			}
			final := runTicks(t, service, run.ID, 3)
			if final.Status != constants.WorkflowRunStatusFailed || final.Failure == nil {
				t.Fatal("bad revision endlessly waited instead of stopping", final.Status)
			}
		})
	}
}
