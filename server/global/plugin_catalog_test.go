package global

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
)

func TestPluginCatalogBackgroundFailurePublishesOneStableNotice(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "catalog.json"), []byte(`{"schemaVersion":1,"entries":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	defer hub.Close()
	subscription := hub.Subscribe(constants.PluginEventTopic, events.Resource{Kind: "global"})
	defer subscription.Close()
	stop := catalog.Start(context.Background(), source, t.TempDir(), func() { publishPluginCatalogChange(hub, source) })
	defer stop()
	select {
	case event := <-subscription.Events:
		if event.Type != "plugins.catalog.sync.succeeded" {
			t.Fatal("startup produced a false failure", event)
		}
	case <-time.After(time.Second):
		t.Fatal("startup did not publish its result")
	}
	if err := os.Remove(filepath.Join(source, "catalog.json")); err != nil {
		t.Fatal(err)
	}
	if err := catalog.RefreshInBackground(source); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events:
		var payload map[string]string
		if event.Type != "plugins.catalog.sync.failed" || json.Unmarshal(event.Payload, &payload) != nil || payload["errorCode"] != status_error.PluginStorageError.Key() {
			t.Fatal("background failure lost its stable notice", event)
		}
	case <-time.After(time.Second):
		t.Fatal("background failure was silent")
	}
	if catalog.State(source).State != "stale" {
		t.Fatal("failure discarded the prior snapshot")
	}
	select {
	case event := <-subscription.Events:
		t.Fatal("one failure produced duplicate events", event)
	default:
	}
}
