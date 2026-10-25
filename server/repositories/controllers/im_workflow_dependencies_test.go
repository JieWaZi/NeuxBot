package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestIMDeletionConfirmsCurrentReferencesAndProtectsActiveFrozenVersion(t *testing.T) {
	db := newTestDB(t)
	store := NewIMController(db)
	ctx := context.Background()
	sameError := func(err error, expected status_error.StatusError) bool {
		var detail statuserror.CommonError
		return errors.As(err, &detail) && detail.Key() == expected.Key()
	}
	if err := store.SaveConnection(ctx, models.IMConnection{ID: "connection", Revision: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.DiscoverBinding(ctx, models.IMBinding{ID: "target", ConnectionID: "connection", ExternalChatID: "user"}); err != nil {
		t.Fatal(err)
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'依赖流程',description:'发送',phases:[{id:'delivery-phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送',description:'发送微信',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]}; export const actions={send:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'target',content:'正文'}})};return await action('send',{id:'deliver',label:'发送微信',phase:'delivery-phase'});`
	compiled, _, err := workflowdsl.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&models.WorkflowDefinition{ID: "flow", Title: "依赖流程", CurrentRevisionID: "revision"},
		&models.WorkflowRevision{ID: "revision", DefinitionID: "flow", Source: source, Manifest: compiled.Manifest, Bundle: compiled.Bundle},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	impact, err := store.ConnectionDependencies(ctx, "connection")
	if err != nil || len(impact.Workflows) != 1 || impact.Workflows[0].NodeID != "deliver" {
		t.Fatalf("当前依赖遗漏: %+v %v", impact, err)
	}
	if err := store.DeleteConnection(ctx, "connection", 1, ""); !sameError(err, status_error.IMConnectionHasWorkflowDependencies) {
		t.Fatalf("未确认也删除: %v", err)
	}
	if err := db.Create(&models.WorkflowRun{ID: "running", DefinitionID: "flow", RevisionID: "revision", Status: constants.WorkflowRunStatusQueued}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowRevision{ID: "new", DefinitionID: "flow", Ordinal: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDefinition{}).Where("id = ?", "flow").Update("current_revision_id", "new").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConnection(ctx, "connection", 1, impact.Digest); !sameError(err, status_error.IMConnectionInUse) {
		t.Fatalf("活跃旧版本未保护: %v", err)
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", "running").Update("status", constants.WorkflowRunStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	parentSource := `export const meta={sdk:'ally.workflow/1',title:'父流程',description:'调用旧版本',phases:[{id:'parent-phase',title:'运行',detail:'调用子流程',canExit:true}]};return await workflow('flow',{id:'child-call',label:'子流程发送',phase:'parent-phase',revisionId:'revision'});`
	parent, _, err := workflowdsl.Compile(parentSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&models.WorkflowDefinition{ID: "parent", Title: "父流程", CurrentRevisionID: "parent-revision"},
		&models.WorkflowRevision{ID: "parent-revision", DefinitionID: "parent", Manifest: parent.Manifest, Bundle: parent.Bundle},
		&models.WorkflowRun{ID: "parent-running", DefinitionID: "parent", RevisionID: "parent-revision", Status: constants.WorkflowRunStatusQueued},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	impact, err = store.ConnectionDependencies(ctx, "connection")
	if err != nil || len(impact.Workflows) != 1 || impact.Workflows[0].RunID != "parent-running" || impact.Workflows[0].NodeID != "child-call" {
		t.Fatalf("子流程尚未启动时遗漏父运行依赖: %+v %v", impact, err)
	}
	if err := store.DeleteConnection(ctx, "connection", 1, impact.Digest); !sameError(err, status_error.IMConnectionInUse) {
		t.Fatalf("父运行仍在使用固定子版本也删除: %v", err)
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", "parent-running").Update("status", constants.WorkflowRunStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDefinition{}).Where("id = ?", "parent").Update("current_revision_id", "").Error; err != nil {
		t.Fatal(err)
	}
	impact, err = store.ConnectionDependencies(ctx, "connection")
	if err != nil || len(impact.Workflows) != 0 {
		t.Fatalf("历史引用仍阻止删除: %+v %v", impact, err)
	}
	if err := db.Create(&models.IMDelivery{ID: "pending", ConnectionID: "connection", RequestID: "pending", State: constants.IMDeliveryStatePending}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConnection(ctx, "connection", 1, ""); !sameError(err, status_error.IMConnectionInUse) {
		t.Fatalf("待发送消息未保护: %v", err)
	}
	if err := db.Model(&models.IMDelivery{}).Where("id = ?", "pending").Update("state", constants.IMDeliveryStateSent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDefinition{}).Where("id = ?", "flow").Update("current_revision_id", "revision").Error; err != nil {
		t.Fatal(err)
	}
	impact, err = store.ConnectionDependencies(ctx, "connection")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDefinition{}).Where("id = ?", "flow").Update("title", "新的影响清单").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConnection(ctx, "connection", 1, impact.Digest); !sameError(err, status_error.IMConnectionHasWorkflowDependencies) {
		t.Fatalf("清单变化后旧确认仍有效: %v", err)
	}
	impact, err = store.ConnectionDependencies(ctx, "connection")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConnection(ctx, "connection", 1, impact.Digest); err != nil {
		t.Fatalf("解除依赖后无法删除: %v", err)
	}
}
