package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
)

func TestFirstSendWithoutDirectoryCreatesPerChatWorkspaceAndContinuationKeepsIt(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	ctx := context.Background()
	input := testMessageInput{ClientRequestID: "managed-first", NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "使用会话临时目录"}, Runtime: runtimeSelection{RuntimeID: runtimecontract.RuntimeIDCodex}}
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
