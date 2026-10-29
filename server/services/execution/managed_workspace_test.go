package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestFirstSendWithoutDirectoryCreatesPerChatWorkspaceAndContinuationKeepsIt(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	ctx := context.Background()
	input := testMessageInput{ClientRequestID: "managed-first", NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "使用会话临时目录"}, Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex}}
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
	if filepath.Clean(second.Chat.WorkspaceDirectory) == filepath.Clean(first.Chat.WorkspaceDirectory) {
		t.Fatal("different chats shared a temporary directory")
	}
	_, err = manager.sendTestMessage(ctx, testMessageInput{ChatID: first.Chat.ID, Message: userMessage{Text: "继续使用原目录"}, Runtime: input.Runtime})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if len(runtime.requests) != 3 || runtime.requests[0].Workspace.Path != runtime.requests[2].Workspace.Path {
		t.Fatalf("continuation changed managed cwd: %+v", runtime.requests)
	}
}

func TestHostDirectoriesFreezeRealPathsAndPreserveBusinessScope(t *testing.T) {
	for _, projectScope := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "project"}[projectScope], func(t *testing.T) {
			ctx := context.Background()
			runtime := &fakeRuntime{}
			manager := newMessageTestService(t, t.TempDir(), runtime)
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
			intent := WorkIntent{ClientRequestID: "host-scope", Chat: ChatTarget{Create: create}, Input: WorkInput{Actor: "user", Content: "处理业务资料"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
			accepted, err := manager.execution.Start(ctx, intent)
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			want := []string{business, internal}
			if !slices.Equal(accepted.Chat.AdditionalWorkingDirectories, want) || accepted.Chat.ProjectID != create.ProjectID {
				t.Fatalf("host scope lost business identity or duplicated real paths: %+v", accepted.Chat)
			}
			request := runtime.requests[0]
			if request.Workspace.Path != accepted.Chat.WorkspaceDirectory || !slices.Equal(request.Workspace.AdditionalWritableDirectories, want) || len(request.Workspace.ProjectDirectories) != 2 {
				t.Fatalf("runtime did not receive the frozen scope: %+v", request.Workspace)
			}
			for _, project := range request.Workspace.ProjectDirectories {
				actual, err := filepath.EvalSymlinks(filepath.Join(request.Workspace.Path, project.Path))
				if err != nil || !slices.Contains(want, actual) {
					t.Fatalf("authorized directory is not reachable through its managed project link: %q %v", actual, err)
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
			if !slices.Equal(runtime.requests[1].Workspace.AdditionalWritableDirectories, want) {
				t.Fatal("continuation did not preserve the frozen host scope")
			}
			if err := os.Remove(internal); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: accepted.Chat.ID}, Input: WorkInput{Actor: "user", Content: "失效目录"}, Options: intent.Options, Admission: intent.Admission}); !errors.Is(err, status_error.ProjectSourceUnavailable) {
				t.Fatalf("continuation silently ignored an unavailable host directory: %v", err)
			}
		})
	}
}

func TestExternalJSONCannotGrantAdditionalChatDirectories(t *testing.T) {
	var create CreateChatSpec
	if err := json.Unmarshal([]byte(`{"WakerID":"waker-1","AdditionalWorkingDirectories":["/unapproved"]}`), &create); err != nil {
		t.Fatal(err)
	}
	if len(create.AdditionalWorkingDirectories) != 0 {
		t.Fatal("external JSON expanded the host-only workspace scope")
	}
}
