package knowledge

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/neux-knowledge/core"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestLiteOrganizeAcceptsModelWithoutPartnerOrChat(t *testing.T) {
	s, lib := newServiceFixture(t)
	receipt, frozen, err := s.Organize(context.Background(), OrganizeCommand{RequestID: "independent-model-task", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if err != nil {
		t.Fatalf("模型选择不应要求伙伴或聊天: %v", err)
	}
	if receipt == nil || receipt.State != "no-op" || frozen.RuntimeID == "" {
		t.Fatalf("非独立接纳: %+v %+v", receipt, frozen)
	}
}

func TestLiteExecutionDefaultsExposeOnlyModelSelection(t *testing.T) {
	raw, err := json.Marshal(models.LibraryExecutionDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "wakerId") {
		t.Fatalf("知识模型偏好包含伙伴: %s", raw)
	}
}

// blockedKnowledgeModel 验证同步模型的取消来自知识作业上下文。
type blockedKnowledgeModel struct {
	entered chan core.GenerateRequest // 暴露实际首次模型请求。
	stopped chan struct{}             // 确认本次调用已经返回。
}

func (model *blockedKnowledgeModel) Generate(ctx context.Context, request core.GenerateRequest) (core.GenerateResult, error) {
	select {
	case model.entered <- request:
	default:
	}
	<-ctx.Done()
	close(model.stopped)
	return core.GenerateResult{}, ctx.Err()
}

func TestLiteServiceShutdownCancelsModelAndPreservesTaskWithoutChat(t *testing.T) {
	s, library, _, db := newServiceFixtureWithDB(t)
	ctx := context.Background()
	source, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "independent-source", LibraryID: library.ID, Filename: "evidence.md", Bytes: []byte("# 来源\n知识整理必须保存此处的真实来源与事实。")})
	if err != nil {
		t.Fatal(err)
	}
	model := &blockedKnowledgeModel{entered: make(chan core.GenerateRequest, 1), stopped: make(chan struct{})}
	s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) { return model, nil }
	receipt, frozen, err := s.Organize(ctx, OrganizeCommand{RequestID: "independent-active-job", LibraryID: library.ID, Message: "优先保留本次要求的离线限制", SourceVersionIDs: []string{source.ResourceID}, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex, AccountID: "account", Model: "model"}})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.RuntimeID == "" {
		t.Fatalf("后台任务继承伙伴或聊天: %+v", frozen)
	}
	s.config.Events = events.New()
	subscription := s.config.Events.Subscribe("knowledge", events.Resource{})
	defer subscription.Close()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case request := <-model.entered:
		if request.JobID != receipt.JobID || request.SourceVersionID != source.ResourceID || request.StageID == "" || request.Thinking {
			t.Fatalf("错误生成请求: %+v", request)
		}
		var messages strings.Builder
		for _, message := range request.Messages {
			messages.WriteString(message.Content)
		}
		if !strings.Contains(messages.String(), "优先保留本次要求的离线限制") {
			t.Fatal("已冻结的本次整理要求没有进入模型输入")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("知识任务没有直接调用模型")
	}
	select {
	case event := <-subscription.Events:
		if event.Type != "knowledge.changed" || event.Resource.ID != library.ID {
			t.Fatalf("错误来源进度事件: %+v", event)
		}
	default:
		t.Fatal("模型仍在生成时，已开始的来源没有通知列表刷新")
	}
	var chats int64
	if err := db.Model(&models.Chat{}).Count(&chats).Error; err != nil || chats != 0 {
		t.Fatalf("整理创建聊天: count=%d error=%v", chats, err)
	}
	s.Close()
	select {
	case <-model.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("关闭知识服务没有取消模型请求")
	}
	final, err := s.config.Engine.GetJob(ctx, receipt.JobID)
	if err != nil || final.State != "processing" || final.CancelRequested {
		t.Fatalf("关闭服务没有保留可恢复任务: %+v %v", final, err)
	}
}

func TestLiteModelPreparationFailureSettlesSourcesAndReleasesLibrary(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	source, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "prepare-failure-source", LibraryID: library.ID, Filename: "source.md", Bytes: []byte("真实来源")})
	if err != nil {
		t.Fatal(err)
	}
	s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
		return nil, status_error.RuntimeUnavailable
	}
	first, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "prepare-failure-job", LibraryID: library.ID, SourceVersionIDs: []string{source.ResourceID}, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.workers.Wait()
	job, err := s.config.Engine.GetJob(ctx, first.JobID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.config.Engine.GetSource(ctx, library.ID, source.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "failed" || original.State != "failed" || original.Diagnostic == "" {
		t.Fatalf("模型准备失败未结算: job=%+v source=%+v", job, original)
	}
	next, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "prepare-failure-retry", LibraryID: library.ID, SourceVersionIDs: []string{source.ResourceID}, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if err != nil || next.JobID == first.JobID {
		t.Fatalf("失败仍占用知识库槽: %+v %v", next, err)
	}
}
