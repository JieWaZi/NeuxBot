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
			Bindings: service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", nil, nil),
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
	bindings := map[string]capability.Binding{}
	for _, value := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", []string{root}, hub) {
		bindings[value.Operation] = value
	}
	request := capability.Request{Operation: "im.deliveries.create", RequestID: "report", Params: map[string]any{"targetId": "b", "files": []string{path}}}
	first, err := bindings[request.Operation].Call(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
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
	for _, binding := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker", WorkspaceDirectory: root}, "execution", []string{root}, nil) {
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
	for _, binding := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", roots, nil) {
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
