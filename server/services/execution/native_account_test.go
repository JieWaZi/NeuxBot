package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

func TestNativeProductRealAccountsDoNotDiagnoseBeforeSending(t *testing.T) {
	for _, provider := range []string{"cursor", "claude", "codex", "workbuddy", "qoder"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "CURSOR_CONFIG_DIR", "CURSOR_DATA_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "APPDATA", "CODEBUDDY_CONFIG_DIR", "QODER_CONFIG_DIR",
				"OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_OAUTH_TOKEN", "CURSOR_API_KEY", "QODER_PERSONAL_ACCESS_TOKEN", "QODER_API_KEY", "CODEBUDDY_API_KEY", "NEUX_EXECUTION_FIXTURE_ACCOUNT_HOME"} {
				t.Setenv(key, "")
			}
			t.Setenv("AGENT_CLI_CREDENTIAL_STORE", "file")
			t.Setenv("NEUX_EXECUTION_FIXTURE_PROVIDER", provider)
			if err := os.Mkdir(filepath.Join(home, ".codex"), 0700); err != nil {
				t.Fatal(err)
			}
			permission, child := constants.PermissionLevelDefault, "TestNativeExecutionACPChild"
			switch provider {
			case "cursor":
				permission, child = constants.PermissionLevelFullAccess, "TestNativeExecutionClaudeCursorChild"
			case "claude":
				permission, child = constants.PermissionLevelAuto, "TestNativeExecutionClaudeCursorChild"
			case "codex":
				permission, child = constants.PermissionLevelAuto, "TestNativeExecutionCodexChild"
			}
			fixture := newNativeProductFixtureWithAccounts(t, ctx, provider, child, permission, aiaccount.New(nil, t.TempDir()))
			core, chats, store, closeRuntime := fixture.open()
			defer func() { closeRuntime() }()
			options := WorkOptions{RuntimeID: provider, Model: "fixture-model", Reasoning: "high", PermissionLevel: permission,
				SessionPolicy: constants.RuntimeSessionPolicyContinue}
			target := ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}
			sessionID := ""
			for turn := range 3 {
				text := "first-success"
				if provider == "workbuddy" || provider == "qoder" {
					text = []string{acpFirstQuestion, acpSecondQuestion, acpResumeQuestion}[turn]
					if turn > 0 {
						options.Reasoning = "low"
					}
				}
				intent := WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: text}, Options: options}
				if target.Create != nil {
					intent.ClientRequestID = "local-account-first-turn"
				}
				work, err := core.Start(ctx, intent)
				if err != nil {
					trace, _ := os.ReadFile(fixture.trace)
					t.Fatalf("actual native send blocked: %v\n%s", err, trace)
				}
				waitNativeProductStatus(t, ctx, store, work, constants.ExecutionStatusCompleted)
				if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
					t.Fatal(err)
				}
				input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: work.Chat.ID, HostInvocationID: work.Execution.ID})
				if err != nil || consumed == nil || input.Session.SessionID == "" {
					t.Fatalf("missing actual native consumption: %+v %+v %v", input, consumed, err)
				}
				if turn == 0 {
					sessionID = input.Session.SessionID
				} else if input.Session.SessionID != sessionID {
					t.Fatalf("conversation changed native session from %s to %s", sessionID, input.Session.SessionID)
				}
				target = ChatTarget{ChatID: work.Chat.ID}
				if turn == 1 {
					if got := fixture.accounts.Load(); got != 1 {
						t.Fatalf("two turns acquired %d physical account leases, want one", got)
					}
					closeRuntime()
					core, chats, store, closeRuntime = fixture.open()
				}
			}
			if got := fixture.accounts.Load(); got != 2 {
				t.Fatalf("restored conversation acquired %d leases, want two", got)
			}
			if provider == "cursor" {
				work, err := core.Start(ctx, WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: "reject-with-native-auth-error"}, Options: options})
				if err != nil {
					t.Fatalf("native send was blocked before its actual authentication response: %v", err)
				}
				waitNativeProductStatus(t, ctx, store, work, constants.ExecutionStatusFailed)
				outcome, err := chats.ReadExecutionOutcome(ctx, work.Chat.ID, work.Execution.ID)
				if err != nil || outcome.FailureCode != constants.RuntimeFailureCodeAuthRequired {
					t.Fatalf("actual native authentication result = %+v, %v", outcome, err)
				}
			}
			trace, err := os.ReadFile(fixture.trace)
			if err != nil {
				t.Fatal(err)
			}
			recovery := map[string]string{"cursor": `"method":"session/resume"`, "claude": "--resume=", "codex": `"method":"thread/resume"`,
				"workbuddy": `"method":"session/load"`, "qoder": `"method":"session/resume"`}[provider]
			if !strings.Contains(string(trace), recovery) {
				t.Fatalf("missing actual native recovery %q:\n%s", recovery, trace)
			}
			for _, diagnostic := range []string{"launch status", "launch auth status", `"method":"account/read"`, "daemon.account_info", "daemon.account_status"} {
				if strings.Contains(string(trace), diagnostic) {
					t.Fatalf("execution issued account diagnostic %q:\n%s", diagnostic, trace)
				}
			}
		})
	}
}
