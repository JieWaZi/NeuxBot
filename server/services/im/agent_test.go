package im

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestAgentDeliveryUsesExactFilesFromAuthenticatedGrant(t *testing.T) {
	service, _, _ := newIMFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected, sibling := filepath.Join(root, "report.txt"), filepath.Join(root, "sibling.txt")
	for _, path := range []string{selected, sibling} {
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry := capability.New(nil)
	defer registry.Close()
	paths := []string{selected}
	prepare := func(files []string) capability.Grant {
		grant, activate, revoke, err := registry.Prepare(capability.GrantSpec{ExecutionID: "execution", FilePaths: files,
			Bindings: service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", nil, nil, ""),
			Validate: func(context.Context) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(revoke)
		activate()
		return grant
	}
	grant, noReference := prepare(paths), prepare(nil)
	paths[0] = sibling
	call := func(grant capability.Grant, requestID, path string) capability.Response {
		body, _ := json.Marshal(map[string]any{"operation": "im.deliveries.create", "requestId": requestID, "params": map[string]any{"targetId": "b", "files": []string{path}}})
		request := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", bytes.NewReader(body))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Authorization", "Bearer "+grant.Token)
		request.Header.Set(constants.AgentCLIExecutionHeader, "execution")
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, request)
		var decoded capability.Response
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	if response := call(noReference, "unreferenced", selected); response.OK {
		t.Fatal("old file was deliverable without explicit reference")
	}
	if response := call(grant, "sibling", sibling); response.OK {
		t.Fatal("exact file scope expanded to its parent or caller mutated the grant")
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(replacement, []byte("revised in place"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, selected); err != nil {
		t.Fatal(err)
	}
	response := call(grant, "selected", selected)
	if !response.OK {
		t.Fatalf("explicit artifact could not be delivered: %+v", response)
	}
	delivery, found, err := service.config.Store.FindDelivery(t.Context(), "cli:chat:waker:selected")
	if err != nil || !found || len(delivery.Files) != 1 {
		t.Fatalf("actual delivery missing frozen file: %+v %v", delivery, err)
	}
	if data, err := os.ReadFile(delivery.Files[0].Path); err != nil || string(data) != "revised in place" {
		t.Fatalf("delivery did not freeze revised original: %q %v", data, err)
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sibling, selected); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if response := call(grant, "rebound", selected); response.OK {
		t.Fatal("exact path rebound to unreferenced sibling")
	}
}

func TestAgentDeliveryKeepsTheOriginWhileSameChatContinues(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: "shared-chat", WakerID: "waker", WorkspaceDirectory: root}
	link := models.IMConversationLink{ID: "im-origin", BindingID: "b", ChatID: chat.ID}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "report.txt")
	if err := os.WriteFile(file, []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	open := func(executionID, replyLinkID string) (capability.Connection, func()) {
		t.Helper()
		path, activate, finish, err := registry.PrepareConnection(root, chat.ID, "http://127.0.0.1:18080"+constants.AgentCLIPath, capability.GrantSpec{
			ExecutionID: executionID,
			Bindings:    service.AgentBindings(chat, executionID, []string{root}, nil, replyLinkID),
			Validate:    func(context.Context) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(finish)
		connection, err := capability.ReadConnection(path)
		if err != nil {
			t.Fatal(err)
		}
		activate()
		return connection, finish
	}
	request := func(method, path, token, executionID string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		input := httptest.NewRequest(method, constants.AgentCLIPath+path, bytes.NewReader(body))
		input.RemoteAddr = "127.0.0.1:12345"
		input.Header.Set("Authorization", "Bearer "+token)
		input.Header.Set(constants.AgentCLIExecutionHeader, executionID)
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, input)
		return response
	}
	readExecutionID := func(connection capability.Connection) string {
		t.Helper()
		response := request(http.MethodGet, constants.AgentCLIExecutionPath, connection.Token, "", nil)
		var result capability.Response
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.OK {
			t.Fatalf("未取得已接纳执行身份: %s (%v)", response.Body.String(), err)
		}
		executionID, _ := result.Data.(string)
		return executionID
	}
	fileCommand, err := json.Marshal(capability.Request{
		Operation: "im.deliveries.create", RequestID: "report",
		Params: map[string]any{"files": []string{file}},
	})
	if err != nil {
		t.Fatal(err)
	}
	desktop, finishDesktop := open("desktop-turn", "")
	desktopID := readExecutionID(desktop)
	if desktopID != "desktop-turn" {
		t.Fatalf("桌面命令取得了错误的执行身份: %q", desktopID)
	}
	finishDesktop()
	im, _ := open("im-turn", link.ID)
	if im.Token != desktop.Token {
		t.Fatal("续聊没有复用原连接凭据")
	}
	for _, oldExecutionID := range []string{desktopID, ""} {
		stale := request(http.MethodPost, "/commands", desktop.Token, oldExecutionID, fileCommand)
		if stale.Code != http.StatusUnauthorized {
			t.Fatalf("旧桌面命令被当前 IM 回合接纳: %d %s", stale.Code, stale.Body.String())
		}
	}
	key := "cli:" + chat.ID + ":" + chat.WakerID + ":report"
	if _, found, err := service.config.Store.FindDelivery(ctx, key); err != nil || found {
		t.Fatalf("已拒绝的命令写入了发送队列: found=%t error=%v", found, err)
	}
	fresh := request(http.MethodPost, "/commands", im.Token, readExecutionID(im), fileCommand)
	if fresh.Code != http.StatusOK {
		t.Fatalf("本轮文件回传失败: %d %s", fresh.Code, fresh.Body.String())
	}
	delivery, found, err := service.config.Store.FindDelivery(ctx, key)
	if err != nil || !found || delivery.LinkID != link.ID || delivery.ExecutionID != "im-turn" {
		t.Fatalf("本轮文件回传归属错误: %+v %v", delivery, err)
	}
}

func TestAgentDeliveryFreezesBytesReplaysAndWakesOnCommittedState(t *testing.T) {
	service, db, platform := newIMFixture(t)
	hub := events.New()
	defer hub.Close()
	service.config.Store = controllers.NewIMController(controllers.WithEventPublisher(db, hub))
	root := t.TempDir()
	path := filepath.Join(root, "report.txt")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := models.IMConversationLink{ID: "origin-link", BindingID: "b", ChatID: "chat"}
	if err := service.config.Store.SaveLink(t.Context(), link); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]capability.Binding{}
	for _, value := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", []string{root}, hub, link.ID) {
		bindings[value.Operation] = value
	}
	request := capability.Request{Operation: "im.deliveries.create", RequestID: "report", Params: map[string]any{"files": []string{path}}}
	first, err := bindings[request.Operation].Call(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Params["targetId"] = "b"
	replay, err := bindings[request.Operation].Call(context.Background(), request)
	if err != nil || !replay.Replayed || replay.CommandID != first.CommandID {
		t.Fatalf("delivery replay: %+v %v", replay, err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings[request.Operation].Call(context.Background(), request); err == nil {
		t.Fatal("same path with changed bytes reused receipt")
	}
	if len(platform.ids) != 0 {
		t.Fatal("CLI acceptance performed an inline external send")
	}
	delivery, err := service.config.Store.Delivery(context.Background(), first.CommandID)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.BindingID != link.BindingID || delivery.LinkID != link.ID {
		t.Fatalf("implicit file reply lost the original IM relationship: %+v", delivery)
	}
	if content, err := os.ReadFile(delivery.Files[0].Path); err != nil || string(content) != "first" {
		t.Fatalf("frozen content changed: %q %v", content, err)
	}
	delivery.State = constants.IMDeliveryStateUnknown
	if err := service.config.Store.UpdateDelivery(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}
	observation, err := bindings["im.deliveries.wait"].Call(context.Background(), capability.Request{Params: map[string]any{"deliveryId": delivery.ID, "timeoutMs": 100}})
	if err != nil || observation.Data.(map[string]any)["completed"] != true {
		t.Fatalf("unknown is a terminal observation: %+v %v", observation, err)
	}
	if _, total, err := service.config.Store.Deliveries(context.Background(), "", 100, 0); err != nil || total != 1 {
		t.Fatalf("duplicate deliveries: %d %v", total, err)
	}
}

func TestAgentDeliveryReplySurvivesTeamMembershipChanges(t *testing.T) {
	service, db, _ := newIMFixture(t)
	service.config.Channels = controllers.NewChannelController(db)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "original-team-link", BindingID: "b", ChatID: "lead-chat", ThreadID: "thread"}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	binding, err := service.config.Store.Binding(ctx, link.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	binding.WakerID, binding.ChannelID, binding.ActiveLinkID = "", "edited-roster", "another-link"
	if err := service.config.Store.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "report.txt")
	if err := os.WriteFile(path, []byte("report from accepted team work"), 0o600); err != nil {
		t.Fatal(err)
	}
	bindings := make(map[string]capability.Binding)
	for _, bound := range service.AgentBindings(models.Chat{ID: "member-chat", WakerID: "original-member"}, "execution", []string{root}, service.config.Events, link.ID) {
		bindings[bound.Operation] = bound
	}
	create := bindings["im.deliveries.create"]
	params := map[string]any{"files": []string{path}}
	result, err := create.Call(ctx, capability.Request{RequestID: "reply", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	params["targetId"] = binding.ID
	if _, err := create.Call(ctx, capability.Request{RequestID: "explicit", Params: params}); !hasCode(err, status_error.IMChatNotAuthorized) {
		t.Fatalf("reply route unexpectedly granted permission for explicit sends: %v", err)
	}
	delivery, err := service.config.Store.Delivery(ctx, result.CommandID)
	if err != nil || delivery.LinkID != link.ID || delivery.ChatID != "member-chat" || delivery.BindingID != binding.ID {
		t.Fatalf("file reply lost the original link or the source member: %+v, %v", delivery, err)
	}
	if err := service.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	delivery, err = service.config.Store.Delivery(ctx, result.CommandID)
	if err != nil || len(delivery.Receipts) != 1 {
		t.Fatalf("delivery did not preserve its platform receipt: %+v, %v", delivery, err)
	}
	quoted, err := service.config.Store.QuoteLink(ctx, binding.ID, delivery.Receipts[0])
	if err != nil || quoted != link.ID {
		t.Fatalf("quoting the artifact did not reopen its original conversation: %q, %v", quoted, err)
	}
	if receipt, err := create.Receipt(ctx, "reply"); err != nil || receipt.CommandID != delivery.ID {
		t.Fatalf("reply receipt lost its authorization: %+v, %v", receipt, err)
	}
	for _, operation := range []string{"im.deliveries.get", "im.deliveries.wait"} {
		if _, err := bindings[operation].Call(ctx, capability.Request{Params: map[string]any{"deliveryId": delivery.ID, "timeoutMs": 100}}); err != nil {
			t.Fatalf("%s could not read the accepted reply: %v", operation, err)
		}
	}
	binding, err = service.config.Store.Binding(ctx, binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	binding.State = constants.IMBindingStateRejected
	if err := service.config.Store.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := create.Receipt(ctx, "reply"); !hasCode(err, status_error.IMChatNotAuthorized) {
		t.Fatalf("revoked binding still exposed reply receipt: %v", err)
	}
	delete(params, "targetId")
	if _, err := create.Call(ctx, capability.Request{RequestID: "rejected", Params: params}); !hasCode(err, status_error.IMChatNotAuthorized) {
		t.Fatalf("revoked binding accepted a new reply: %v", err)
	}
}

func TestAgentDeliveryExcludesInternalSpaceFiles(t *testing.T) {
	service, _, _ := newIMFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(root, ".claude", "settings.json")
	if err := os.Mkdir(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("internal configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "settings.json")
	if err := os.Symlink(settings, alias); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(root, "report.txt")
	if err := os.WriteFile(note, []byte("business report"), 0600); err != nil {
		t.Fatal(err)
	}
	internalAlias := filepath.Join(root, ".claude", "report.txt")
	if err := os.Symlink(note, internalAlias); err != nil {
		t.Fatal(err)
	}
	blocked := []string{settings, alias, internalAlias}
	caseAlias := filepath.Join(root, ".CLAUDE", "settings.json")
	if _, err := os.Stat(caseAlias); err == nil {
		blocked = append(blocked, caseAlias)
	}
	for _, binding := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker", WorkspaceDirectory: root}, "execution", []string{root}, nil, "") {
		if binding.Operation != "im.deliveries.create" {
			continue
		}
		for _, path := range blocked {
			_, err := binding.Call(t.Context(), capability.Request{Operation: binding.Operation, RequestID: "internal", Params: map[string]any{"targetId": "b", "files": []string{path}}})
			if err != status_error.IMFileInvalid {
				t.Fatalf("internal space file %q was delivered: %v", path, err)
			}
		}
		return
	}
	t.Fatal("IM delivery capability is missing")
}

func TestAgentDeliveryReadsExactOutputAndProjectRootsAndRejectsOtherPrivateFiles(t *testing.T) {
	service, _, _ := newIMFixture(t)
	base := t.TempDir()
	output := filepath.Join(base, "Agents", "waker", "outputs", "execution")
	project := filepath.Join(base, "project")
	private := filepath.Join(base, "Conversations", "chat")
	sibling := filepath.Join(base, "Agents", "waker", "outputs", "other")
	for _, dir := range []string{output, project, private, sibling} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	roots := []string{output, project}
	var create capability.Binding
	for _, binding := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", roots, nil, "") {
		if binding.Operation == "im.deliveries.create" {
			create = binding
		}
	}
	roots[0] = base
	for index, dir := range []string{output, project, private, sibling} {
		result, err := create.Call(context.Background(), capability.Request{Operation: create.Operation, RequestID: []string{"output", "project", "private", "sibling"}[index], Params: map[string]any{"targetId": "b", "files": []string{filepath.Join(dir, "report.txt")}}})
		if index < 2 {
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := service.config.Store.Delivery(context.Background(), result.CommandID)
			if err != nil {
				t.Fatal(err)
			}
			if content, err := os.ReadFile(delivery.Files[0].Path); err != nil || string(content) != dir {
				t.Fatalf("delivery snapshot: %q %v", content, err)
			}
		} else if err == nil {
			t.Fatalf("ungranted directory accepted: %s", dir)
		}
	}
}
