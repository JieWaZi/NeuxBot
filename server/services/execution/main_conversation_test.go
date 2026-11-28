package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
)

// TestAutomaticAgentReportUsesThePublicationTransport 核验定时报告复用现有工具传输和本轮授权。
func TestAutomaticAgentReportUsesThePublicationTransport(t *testing.T) {
	manager := agentPreparationFixture(t)
	source := manager.journal.(*agentTestJournal).chat
	manager.automaticReportBinding = func(_ context.Context, chat models.Chat, executionID, runID string) (capability.Binding, error) {
		if chat.ID != source.ID || executionID != "turn" || runID != "schedule" {
			t.Fatalf("binding was not given the host-owned identities")
		}
		return capability.Binding{
			Operation: constants.ConversationMessagePostOperation,
			Call: func(context.Context, capability.Request) (capability.Result, error) {
				return capability.Result{}, nil
			},
			Receipt: func(context.Context, string) (capability.Result, error) {
				return capability.Result{}, status_error.AgentReceiptNotFound
			},
		}, nil
	}
	request := pendingMessage{runtime: runtimeSelection{RuntimeID: "codex"}, automaticRunID: "schedule", allowedAgentDomains: &[]string{"conversations"}}
	work := recordedRun{Runtime: request.runtime}
	conn, err := manager.prepareAgent(t.Context(), t.Context(), models.RuntimeEnvironment{}, work.Workspace, &work.Skills, &work.Instructions, &work.Connectors, &work.ContextInstructions, source, "turn", request, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.finish()
	guide := agentapi.DeveloperInstructions(work.Instructions)
	if len(work.Connectors) != 1 || !strings.Contains(guide, "Automatic task delivery") ||
		strings.Contains(guide, "post each") || strings.Contains(guide, constants.AutomaticReportNoChanges) {
		t.Fatalf("automatic report did not use the existing publication transport and its one-report scope: %s", guide)
	}
}

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

func TestMainConversationAndSourceKeepStablePublicationGuidance(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	chat.HiddenFromWakerHistory = true
	var stable string
	for _, origin := range []string{MainConversationEntryPoint, MainConversationSourceEntryPoint} {
		work := recordedRun{Runtime: runtimeSelection{RuntimeID: "codex"}}
		grants := MainConversationTools(chat.ID, origin, "", &mainconversation.Service{})
		request := pendingMessage{runtime: work.Runtime, toolCapabilities: grants, entryPoint: origin, allowedAgentDomains: &[]string{}}
		if origin == MainConversationSourceEntryPoint {
			destination := mainconversation.PrivateSourceInstruction()
			request.contextInstructions = []agentapi.Instruction{{Name: destination.Name, Content: destination.Content}}
		}
		work.Instructions, work.ContextInstructions = nativeInstructionInputs(request, &preparedMessageFiles{})
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
		if !strings.Contains(current, "conversations.messages.post") || !strings.Contains(current, "results, findings or blockers") ||
			!strings.Contains(current, "authorized scope") {
			t.Fatal("both inputs need the bound tool and authority for work outcomes")
		}
		if !strings.Contains(current, "as soon as it is ready") || !strings.Contains(current, "Use a structured question") {
			t.Fatal("both inputs must prompt timely findings and structured questions")
		}
		turn, err := work.turnRequest(origin, work.Detection)
		if err != nil {
			t.Fatal(err)
		}
		if origin == MainConversationEntryPoint && len(turn.Context) != 0 {
			t.Fatalf("public input got a private destination override: %+v", turn.Context)
		}
		if origin == MainConversationSourceEntryPoint {
			if len(turn.Context) != 1 || turn.Context[0].Name != "Input origin" {
				t.Fatalf("source input did not carry exactly one origin note: %+v", turn.Context)
			}
			note, ok := turn.Context[0].Value.(string)
			if !ok || !strings.Contains(note, "send the user-facing reply or outcome there") ||
				!strings.Contains(note, "even for brief chat") || !strings.Contains(note, "Respect requests to reply only here") {
				t.Fatalf("source input did not guide allowed replies back to main: %+v", turn.Context)
			}
		}
	}
}
