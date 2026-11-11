package channel

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestTeamDispatchReadsCurrentProfilesWithoutChangingAdmittedMembership(t *testing.T) {
	fixture := newDeletionFixture(t)
	ctx := context.Background()
	wakers := controllers.NewWakerController(fixture.db)
	createChannelTestWakers(t, ctx, wakers, "lead", "member", "future")
	configured := []models.ChannelMember{{ChannelID: "team", WakerID: "lead", RuntimeID: "codex"}, {ChannelID: "team", WakerID: "member", RuntimeID: "codex", Position: 1}}
	team := models.Channel{ID: "team", Name: "资料刷新团队", LeadWakerID: "lead"}
	if err := fixture.channels.Create(ctx, team, configured); err != nil {
		t.Fatal(err)
	}
	thread := models.ChannelThread{ID: "thread", ChannelID: team.ID, LeadWakerID: "lead", LeadChatID: "lead-chat", ReadyAt: testTimestamp(1), MaxParallel: 2, MaxTasks: 10}
	if err := fixture.threads.Create(ctx, thread, []models.ChannelThreadMember{
		{ThreadID: thread.ID, WakerID: "lead", ChatID: "lead-chat", DisplayName: "历史负责人", IsLead: true, RuntimeID: "codex", Model: "admitted-model"},
		{ThreadID: thread.ID, WakerID: "member", ChatID: "member-chat", DisplayName: "历史成员", RoleName: "历史角色", Bio: "历史简介", RuntimeID: "codex", Position: 1},
		{ThreadID: thread.ID, WakerID: "missing", ChatID: "missing-chat", DisplayName: "缺失伙伴历史名称", RoleName: "历史专员", Bio: "缺失伙伴历史简介", RuntimeID: "codex", Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	chats := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{"lead-chat": {Chat: models.Chat{ID: "lead-chat"}}}}
	executions := &postTurnExecutionFake{}
	sut := fixture.service
	sut.wakers, sut.tasks = wakers, controllers.NewChannelTaskController(fixture.db)
	sut.chats, sut.executions = chats, executions
	sut.capabilities = channeltool.NewRegistry()
	sut.watchers = make(map[string]struct{})
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sut.Close)
	if _, err := sut.teamInstruction(ctx, thread, "lead"); err != nil {
		t.Fatal(err)
	}
	if err := wakers.Update(ctx, "member", models.Waker{Name: "当前成员名称", RoleName: "当前设计师", Bio: "当前交互职责"}); err != nil {
		t.Fatal(err)
	}
	if err := wakers.UpdateProfileProjection(ctx, "member", profile.Projection{Expertise: []profile.Expertise{{Title: "当前专长", Description: "当前可访问性设计"}}}, 2); err != nil {
		t.Fatal(err)
	}
	// 团队配置的负责人、成员及执行默认值变化只服务于之后建立的对话。
	team.LeadWakerID = "future"
	if err := fixture.channels.Update(ctx, team, []models.ChannelMember{{ChannelID: team.ID, WakerID: "future", RuntimeID: "claude", Model: "future-model"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "按当前专长分派"}}); err != nil {
		t.Fatal(err)
	}
	if executions.startCount() != 1 {
		t.Fatalf("执行接纳次数 = %d", executions.startCount())
	}
	intent := executions.firstStart()
	if intent.Chat.ChatID != "lead-chat" || intent.Options.Model != "admitted-model" {
		t.Fatalf("已接纳负责人映射或执行选择被新团队配置覆盖: %+v", intent)
	}
	content := intent.Options.SupplementalInstructions[0].Content
	for _, want := range []string{"当前成员名称", "当前设计师", "当前交互职责", "当前专长: 当前可访问性设计", "缺失伙伴历史名称", "缺失伙伴历史简介"} {
		if !strings.Contains(content, want) {
			t.Fatalf("派发未使用当前资料或缺失伙伴回退，缺少 %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "历史成员") || strings.Contains(content, "`future`") {
		t.Fatalf("资料投影残留旧资料或扩大资格:\n%s", content)
	}
	retained, err := fixture.threads.GetMember(ctx, thread.ID, "member")
	if err != nil || retained.DisplayName != "历史成员" || retained.ChatID != "member-chat" {
		t.Fatalf("当前资料投影改写了历史资料或成员映射: %+v %v", retained, err)
	}
	chats.mutex.Lock()
	chats.summaries["lead-chat"] = chatservice.ChatSummary{Chat: models.Chat{ID: "lead-chat", ActiveExecutionID: "current"}}
	chats.mutex.Unlock()
	lease, _, err := sut.mintLeadCapability(ctx, thread, "current", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range lease.Bindings {
		if binding.Operation != "team.members.list" {
			continue
		}
		result, err := binding.Call(ctx, capability.Request{})
		if err != nil {
			t.Fatal(err)
		}
		items := result.Data.(map[string]any)["items"].([]map[string]any)
		if len(items) != 3 || items[1]["wakerId"] != "member" || items[1]["name"] != "当前成员名称" || items[1]["role"] != "当前设计师" || items[1]["bio"] != "当前交互职责" || items[0]["isLead"] != true || items[2]["name"] != "缺失伙伴历史名称" {
			t.Fatalf("成员发现未使用同一当前资料或改变资格: %+v", items)
		}
		if err := wakers.Update(ctx, "member", models.Waker{Name: "当前成员名称"}); err != nil {
			t.Fatal(err)
		}
		if err := wakers.UpdateProfileProjection(ctx, "member", profile.Projection{}, 3); err != nil {
			t.Fatal(err)
		}
		instruction, err := sut.teamInstruction(ctx, thread, "lead")
		if err != nil {
			t.Fatal(err)
		}
		for _, stale := range []string{"历史角色", "\n  历史简介\n", "当前设计师", "当前交互职责", "当前专长"} {
			if strings.Contains(instruction.Content, stale) {
				t.Fatalf("伙伴存在但资料为空时错误回退旧资料 %q:\n%s", stale, instruction.Content)
			}
		}
		return
	}
	t.Fatal("负责人未获准发现原对话成员")
}

func TestMemberFrontmatterKeepsBoundedPublicProjection(t *testing.T) {
	frontmatter := memberFrontmatter(
		"负责桌面端界面与交互实现。",
		[]profile.Expertise{{
			Title:       "前端交互",
			Description: "设计并实现可访问的桌面端组件。",
		}},
	)
	if !strings.Contains(frontmatter, "负责桌面端界面与交互实现。") ||
		!strings.Contains(frontmatter, "前端交互: 设计并实现可访问的桌面端组件。") {
		t.Fatalf("frontmatter = %q", frontmatter)
	}
	if strings.Contains(frontmatter, "{") || strings.Contains(frontmatter, "[") {
		t.Fatalf("frontmatter unexpectedly embeds JSON: %q", frontmatter)
	}
}

func TestTeamWorkspaceSectionOmitEmptyPath(t *testing.T) {
	if section := teamWorkspaceSection("  "); section != "" {
		t.Fatalf("empty workspace section = %q", section)
	}
	section := teamWorkspaceSection("/tmp/shared")
	if !strings.Contains(section, "/tmp/shared") || !strings.Contains(section, "Team Workspace") {
		t.Fatalf("workspace section = %q", section)
	}
}
