package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestSameWakerChatsShareCWDWithoutPerExecutionDirectories(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := context.Background()
	input := testMessageInput{ClientRequestID: "managed-first", NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "使用会话临时目录"}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex}}
	first, err := manager.sendTestMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if first.Chat.WorkspaceDirectory == "" || len(first.Chat.AdditionalWorkingDirectories) != 1 || first.Chat.AdditionalWorkingDirectories[0] != first.Chat.WorkspaceDirectory {
		t.Fatalf("missing managed scope: %+v", first.Chat)
	}
	if stat, err := os.Stat(first.Chat.WorkspaceDirectory); err != nil || !stat.IsDir() {
		t.Fatalf("workspace was not created: %v", err)
	}
	second, err := manager.sendTestMessage(ctx, testMessageInput{ClientRequestID: "managed-second", NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "另一段会话"}, Runtime: input.Runtime})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if filepath.Clean(second.Chat.WorkspaceDirectory) != filepath.Clean(first.Chat.WorkspaceDirectory) {
		t.Fatal("same partner chats used different cwd")
	}
	for _, name := range []string{"outputs", "tmp"} {
		if _, err := os.Stat(filepath.Join(first.Chat.WorkspaceDirectory, name)); !os.IsNotExist(err) {
			t.Fatalf("shared space created imposed directory %s: %v", name, err)
		}
	}
	if first.Execution.ID == second.Execution.ID {
		t.Fatal("different messages reused execution identity")
	}
	_, err = manager.sendTestMessage(ctx, testMessageInput{ChatID: first.Chat.ID, Message: userMessage{Text: "继续使用原目录"}, Runtime: input.Runtime})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if calls := manager.nativeCalls(t, "turn/start"); len(calls) != 3 || !strings.Contains(string(calls[2]), first.Chat.WorkspaceDirectory) {
		t.Fatalf("continuation changed managed cwd: %s", calls)
	}
}

func TestHostDirectoriesFreezeRealPathsAndPreserveBusinessScope(t *testing.T) {
	for _, projectScope := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "project"}[projectScope], func(t *testing.T) {
			ctx := context.Background()
			manager := newMessageTestService(t, t.TempDir())
			business := filepath.Join(t.TempDir(), "shared")
			internal := filepath.Join(t.TempDir(), "shared")
			for _, directory := range []string{business, internal} {
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			business, err = filepath.EvalSymlinks(business)
			if err != nil {
				t.Fatal(err)
			}
			internal, err = filepath.EvalSymlinks(internal)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "internal-link")
			if err := os.Symlink(internal, alias); err != nil {
				t.Fatal(err)
			}
			create := &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: business, AdditionalWorkingDirectories: []string{internal, alias, business}}
			if projectScope {
				source, err := resolveWorkingDirectory(business)
				if err != nil {
					t.Fatal(err)
				}
				if err := manager.projectSources.Ensure(ctx, source); err != nil {
					t.Fatal(err)
				}
				project := models.Project{ID: util.New(), WakerID: "waker-1", Name: "业务项目"}
				if err := manager.projects.Create(ctx, project); err != nil {
					t.Fatal(err)
				}
				if err := manager.projects.ReplaceDirectories(ctx, project.ID, []models.ProjectDirectory{{Source: source, IsDefault: true}}); err != nil {
					t.Fatal(err)
				}
				create.ProjectID, create.WorkingDirectory = project.ID, ""
			}
			intent := WorkIntent{ClientRequestID: "host-scope", Chat: ChatTarget{Create: create}, Input: WorkInput{Actor: "user", Content: "处理业务资料"}, Options: WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
			accepted, err := manager.execution.Start(ctx, intent)
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			want := []string{business, internal}
			if !slices.Equal(accepted.Chat.AdditionalWorkingDirectories, want) || accepted.Chat.ProjectID != create.ProjectID {
				t.Fatalf("host scope lost business identity or duplicated real paths: %+v", accepted.Chat)
			}
			native := manager.nativeCalls(t, "turn/start")
			if len(native) != 1 || !strings.Contains(string(native[0]), accepted.Chat.WorkspaceDirectory) {
				t.Fatalf("missing actual native working directory: %s", native)
			}
			for _, authorized := range want {
				if !strings.Contains(string(native[0]), authorized) {
					t.Fatalf("missing native writable root %q in %s", authorized, native[0])
				}
			}
			if _, err := manager.execution.Start(ctx, intent); err != nil {
				t.Fatalf("same host scope could not replay: %v", err)
			}
			changed := *create
			changed.AdditionalWorkingDirectories = []string{t.TempDir()}
			intent.Chat.Create = &changed
			if _, err := manager.execution.Start(ctx, intent); !errors.Is(err, status_error.CreateRequestConflict) {
				t.Fatalf("changed host scope reused a committed request: %v", err)
			}
			if _, err := manager.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: accepted.Chat.ID}, Input: WorkInput{Actor: "user", Content: "继续"}, Options: intent.Options, Admission: intent.Admission}); err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			if len(manager.nativeCalls(t, "turn/start")) != 2 {
				t.Fatal("continuation did not preserve the frozen host scope")
			}
			if err := os.Remove(internal); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: accepted.Chat.ID}, Input: WorkInput{Actor: "user", Content: "不需要离线项目的追问"}, Options: intent.Options, Admission: intent.Admission}); err != nil {
				t.Fatalf("offline additional project blocked an unrelated continuation: %v", err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			calls := manager.nativeCalls(t, "turn/start")
			if len(calls) != 3 || strings.Contains(string(calls[2]), internal) {
				t.Fatalf("offline root remained in the effective native scope: %s", calls)
			}
			summary, err := manager.chats.GetChatSummary(ctx, accepted.Chat.ID)
			if err != nil || !slices.Equal(summary.Chat.AdditionalWorkingDirectories, want) {
				t.Fatalf("effective subset rewrote the durable authorization: %+v %v", summary, err)
			}
			reference := WorkInput{Actor: "user", Content: "读取离线文件", CodeReferences: []CodeReferenceInput{{Path: filepath.Join(internal, "notes.md")}}}
			if _, err := manager.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: accepted.Chat.ID}, Input: reference, Options: intent.Options, Admission: intent.Admission}); !errors.Is(err, status_error.ProjectSourceUnavailable) {
				t.Fatalf("explicit offline reference did not report unavailable source: %v", err)
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, internal); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: accepted.Chat.ID}, Input: WorkInput{Actor: "user", Content: "继续讨论原问题"}, Options: intent.Options, Admission: intent.Admission}); err != nil {
				t.Fatalf("repointed optional root blocked unrelated text: %v", err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			calls = manager.nativeCalls(t, "turn/start")
			if len(calls) != 4 || strings.Contains(string(calls[3]), outside) || strings.Contains(string(calls[3]), internal) {
				t.Fatalf("repointed symlink expanded native authorization: %s", calls)
			}
			if _, err := manager.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: accepted.Chat.ID}, Input: reference, Options: intent.Options, Admission: intent.Admission}); !errors.Is(err, status_error.ProjectSourceUnavailable) {
				t.Fatalf("repointed source was implicitly reauthorized: %v", err)
			}
		})
	}
}

func TestExternalJSONCannotGrantAdditionalChatDirectories(t *testing.T) {
	var create CreateChatSpec
	if err := json.Unmarshal([]byte(`{"WakerID":"waker-1","AdditionalWorkingDirectories":["/unapproved"],"ExecutionWorkspaceDirectory":"/unapproved"}`), &create); err != nil {
		t.Fatal(err)
	}
	if len(create.AdditionalWorkingDirectories) != 0 || create.ExecutionWorkspaceDirectory != "" {
		t.Fatal("external JSON expanded the host-only workspace scope")
	}
}
