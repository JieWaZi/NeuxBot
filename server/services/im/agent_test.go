package im

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

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
	bindings := map[string]agentcapability.Binding{}
	for _, value := range service.AgentBindings(models.Chat{ID: "chat", WakerID: "waker"}, "execution", []string{root}, hub) {
		bindings[value.Operation] = value
	}
	request := agentcapability.Request{Operation: "im.deliveries.create", RequestID: "report", Params: map[string]any{"targetId": "b", "files": []string{path}}}
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
	observation, err := bindings["im.deliveries.wait"].Call(context.Background(), agentcapability.Request{Params: map[string]any{"deliveryId": delivery.ID, "timeoutMs": 100}})
	if err != nil || observation.Data.(map[string]any)["completed"] != true {
		t.Fatalf("unknown is a terminal observation: %+v %v", observation, err)
	}
	if _, total, err := service.config.Store.Deliveries(context.Background(), "", 100, 0); err != nil || total != 1 {
		t.Fatalf("duplicate deliveries: %d %v", total, err)
	}
}
