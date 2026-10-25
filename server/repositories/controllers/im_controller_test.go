package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestIMControllerEncryptsSecretsDeduplicatesAndKeepsProjection(t *testing.T) {
	db := newTestDB(t)
	store := NewIMController(db)
	ctx := context.Background()
	connection := models.IMConnection{ID: "c", Name: "微信", Platform: constants.IMPlatformWeixin, AppID: "original-bot", OwnerID: "original-owner", Credential: "private-token", Enabled: true}
	if err := store.SaveConnection(ctx, connection, 0); err != nil {
		t.Fatal(err)
	}
	var raw models.IMConnection
	if err := db.First(&raw, "id = ?", "c").Error; err != nil {
		t.Fatal(err)
	}
	if raw.CredentialCiphertext == "" || strings.Contains(raw.CredentialCiphertext, "private-token") {
		t.Fatal("credential stored unencrypted")
	}
	decoded, err := store.Connection(ctx, "c")
	if err != nil || decoded.Credential != "private-token" {
		t.Fatalf("decrypt: %v %v", decoded.Credential, err)
	}
	encoded, _ := json.Marshal(decoded)
	if strings.Contains(string(encoded), "private-token") || strings.Contains(string(encoded), "credential") {
		t.Fatal("API leaked credentials")
	}
	decoded.Name = "renamed"
	if err := store.SaveConnection(ctx, decoded, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConnection(ctx, decoded, 1); !errors.Is(err, status_error.IMConfigurationConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	changedIdentity := decoded
	changedIdentity.AppID = "another-bot"
	changedIdentity.Credential = "another-token"
	if err := store.SaveConnection(ctx, changedIdentity, 2); !errors.Is(err, status_error.IMConfigurationConflict) {
		t.Fatalf("another bot inherited original authorization: %v", err)
	}
	actualIdentity, _ := store.Connection(ctx, "c")
	if actualIdentity.Credential != "private-token" || actualIdentity.AppID != "original-bot" || actualIdentity.Revision != 2 {
		t.Fatal("identity rejection altered saved credential")
	}
	binding := models.IMBinding{ID: "b", ConnectionID: "c", ExternalChatID: "user", State: constants.IMBindingStateApproved, ContextToken: "private-context", Revision: 1, ControllerIDs: []string{"owner"}}
	if err := store.DiscoverBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	binding.State = constants.IMBindingStatePending
	binding.ContextToken = "new-context"
	if err := store.DiscoverBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	current, err := store.Binding(ctx, "b")
	if err != nil || current.State != constants.IMBindingStateApproved || current.ContextToken != "new-context" {
		t.Fatalf("discovery changed authorization: %+v %v", current, err)
	}
	input := models.IMInbox{ID: "i", ConnectionID: "c", BindingID: "b", ExternalMessageID: "message", State: constants.IMInboxStatePending, Files: []models.IMFile{{Key: "private-cdn", AESKey: "private-aes", Name: "photo.png"}}}
	if err := store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.Content = "duplicate altered"
	if err := store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	var stored models.IMInbox
	if err := db.First(&stored, "id = ?", "i").Error; err != nil {
		t.Fatal(err)
	}
	if stored.Content != "" || stored.FilesCiphertext == "" || strings.Contains(stored.FilesCiphertext, "private-") {
		t.Fatal("duplicate overwrite or unencrypted media")
	}
	inputs, err := store.Inbox(ctx, constants.IMInboxStatePending, 10)
	if err != nil || len(inputs) != 1 || inputs[0].Files[0].AESKey != "private-aes" {
		t.Fatalf("media recovery: %+v %v", inputs, err)
	}
	link := models.IMConversationLink{ID: "l", BindingID: "b"}
	if err := store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	link.EventCursor = 10
	link.PendingReportIDs = []string{"report"}
	if err := store.CheckpointLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	link.EventCursor = 0
	link.ChatID = "chat"
	link.PendingReportIDs = nil
	if err := store.UpdateLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	actual, err := store.Link(ctx, "l")
	if err != nil || actual.ChatID != "chat" || actual.EventCursor != 10 || len(actual.PendingReportIDs) != 1 {
		t.Fatalf("relation overwrote projection: %+v %v", actual, err)
	}
	if err := store.CheckpointLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	actual, _ = store.Link(ctx, "l")
	if actual.EventCursor != 10 {
		t.Fatal("checkpoint regressed")
	}
}

func TestIMDeliveryRecoveryAndQuoteStayWithinChat(t *testing.T) {
	store := NewIMController(newTestDB(t))
	ctx := context.Background()
	delivery := models.IMDelivery{ID: "d", RequestID: "request", BindingID: "b", LinkID: "l", State: constants.IMDeliveryStatePending, Receipts: []string{}}
	if err := store.CreateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimDelivery(ctx, "d")
	if err != nil || !claimed {
		t.Fatal(err)
	}
	claimed, _ = store.ClaimDelivery(ctx, "d")
	if claimed {
		t.Fatal("duplicate claim")
	}
	if err := store.RecoverDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	delivery, err = store.Delivery(ctx, "d")
	if err != nil || delivery.State != constants.IMDeliveryStateUnknown || delivery.Attempts != 1 {
		t.Fatalf("recovery: %+v %v", delivery, err)
	}
	delivery.Receipts = []string{"platform-message"}
	if err := store.UpdateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	link, err := store.QuoteLink(ctx, "b", "platform-message")
	if err != nil || link != "l" {
		t.Fatalf("quote: %s %v", link, err)
	}
	link, err = store.QuoteLink(ctx, "another-chat", "platform-message")
	if err != nil || link != "" {
		t.Fatal("quote escaped chat scope")
	}
}

func TestIMConnectionsCountOnlyExistingConversationsAcrossBindings(t *testing.T) {
	db := newTestDB(t)
	store := NewIMController(db)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		if err := store.SaveConnection(ctx, models.IMConnection{ID: id, Name: id}, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, binding := range []models.IMBinding{{ID: "a", ConnectionID: "first", ExternalChatID: "a"}, {ID: "b", ConnectionID: "first", ExternalChatID: "b"}, {ID: "c", ConnectionID: "second", ExternalChatID: "c"}} {
		if err := store.DiscoverBinding(ctx, binding); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"chat-a", "chat-b", "chat-c"} {
		if err := db.Create(&models.Chat{ID: id, AdditionalWorkingDirectories: []string{}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, link := range []models.IMConversationLink{{ID: "a", BindingID: "a", ChatID: "chat-a"}, {ID: "b", BindingID: "b", ChatID: "chat-b"}, {ID: "c", BindingID: "c", ChatID: "chat-c"}, {ID: "draft", BindingID: "a"}, {ID: "deleted", BindingID: "a", ChatID: "missing"}} {
		if err := store.SaveLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}
	values, err := store.Connections(ctx)
	if err != nil || len(values) != 2 || values[0].ConversationCount != 2 || values[1].ConversationCount != 1 {
		t.Fatalf("counts = %+v, err = %v", values, err)
	}
	if db.Migrator().HasColumn(&models.IMConnection{}, "conversation_count") || db.Migrator().HasColumn(&models.IMConversationLink{}, "turn_count") {
		t.Fatal("query counts were persisted as duplicate counters")
	}
}
