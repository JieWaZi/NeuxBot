package execution

import (
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
)

// TestConversationWorkerReceivesItsPublicationTransportAndInstructions 核验发言授权、原生工具传输和模型提示同步生效。
func TestConversationWorkerReceivesItsPublicationTransportAndInstructions(t *testing.T) {
	for _, tc := range []struct {
		// runtime 是执行选用的运行时。
		runtime string
		// grant 表示本轮是否获授主会话发言权。
		grant bool
		// stdio 表示本轮是否应使用内部 MCP 传输。
		stdio bool
	}{
		{"codex", true, true},
		{"unsupported", true, false},
		{"codex", false, false},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			manager := agentPreparationFixture(t)
			chat := manager.journal.(*agentTestJournal).chat
			var grants []ToolCapability
			if tc.grant {
				chat.HiddenFromWakerHistory = true
				grants = MainConversationTools(chat.ID, "turn", "public-input", &mainconversation.Service{})
			}
			work := recordedRun{Runtime: runtimeSelection{RuntimeID: tc.runtime}}
			_, release, err := work.prepare(manager, chat, "turn", grants, &[]string{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if got := len(work.Connectors) == 1 && work.Connectors[0].RuntimeKey == "bot_capabilities"; got != tc.stdio {
				t.Fatalf("internal MCP attached = %v, want %v", got, tc.stdio)
			}
			if got := strings.Contains(agentapi.DeveloperInstructions(work.Instructions), "Public conversation messages"); got != tc.grant {
				t.Fatalf("publication instructions present = %v, want %v", got, tc.grant)
			}
			if got := strings.Contains(agentapi.DeveloperInstructions(work.Instructions), "completed=false"); got != tc.grant {
				t.Fatalf("pending question instructions present = %v, want %v", got, tc.grant)
			}
			if !tc.stdio && work.CLIContextFile == "" {
				t.Fatal("CLI fallback has no host grant")
			}
		})
	}
}

// TestConversationPublicationGrantRejectsMovedSourceOrTurn 验证已签发的发言能力不能转用到其他 Chat 和回合。
func TestConversationPublicationGrantRejectsMovedSourceOrTurn(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	chat.HiddenFromWakerHistory = true
	publisher := &mainconversation.Service{}
	for _, tc := range []struct {
		// description 标识本轮 Chat 或执行与签发来源的差异。
		description string
		// workChat 是业务入口冻结的工作 Chat。
		workChat string
		// execution 是业务入口冻结的执行。
		execution string
		// hidden 表示实际 Chat 是否为隐藏的工作记录。
		hidden bool
	}{
		{"other chat", "other-chat", "turn", true},
		{"other turn", chat.ID, "other-turn", true},
		{"ordinary chat", chat.ID, "turn", false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			chat.HiddenFromWakerHistory = tc.hidden
			grants := MainConversationTools(tc.workChat, tc.execution, "public-input", publisher)
			work := recordedRun{Runtime: runtimeSelection{RuntimeID: "codex"}}
			_, release, err := work.prepare(manager, chat, "turn", grants, &[]string{}, nil)
			if release != nil {
				release()
			}
			if !errors.Is(err, status_error.AgentContextUnauthorized) {
				t.Fatalf("mismatched source acquired a publication grant: %v", err)
			}
		})
	}
}

func TestMainConversationAndSourceUseSamePublicDestination(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	chat.HiddenFromWakerHistory = true
	var stable string
	for _, origin := range []string{MainConversationEntryPoint, MainConversationSourceEntryPoint} {
		work := recordedRun{Runtime: runtimeSelection{RuntimeID: "codex"}}
		grants := MainConversationTools(chat.ID, origin, "", &mainconversation.Service{})
		request := pendingMessage{runtime: work.Runtime, toolCapabilities: grants, entryPoint: origin, allowedAgentDomains: &[]string{}}
		conn, err := manager.prepareAgent(t.Context(), t.Context(), models.RuntimeEnvironment{}, work.Workspace, &work.Skills, &work.Instructions, &work.Connectors, &work.ContextInstructions, chat, origin, request, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		conn.finish()
		current := agentapi.DeveloperInstructions(work.Instructions)
		if stable != "" && current != stable {
			t.Fatal("switching between the main conversation and its work history reset session instructions")
		}
		stable = current
		if !strings.Contains(current, "conversations.messages.post") || strings.Contains(current, "answer in the private work transcript") {
			t.Fatal("both inputs must direct answers to the same public conversation")
		}
		if !strings.Contains(current, "as soon as you have it, then continue") || !strings.Contains(current, "Use a structured question") {
			t.Fatal("both inputs must prompt timely findings and structured questions")
		}
	}
}
