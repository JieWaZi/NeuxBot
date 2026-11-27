package events

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
)

func TestMainConversationSubscriptionRequiresExactSubject(t *testing.T) {
	for _, resource := range []eventhub.Resource{{Kind: "waker", ID: "a"}, {Kind: "team", ID: "a"}} {
		if !validResourceShape("main-conversations", resource) {
			t.Fatalf("subject rejected: %#v", resource)
		}
	}
	for _, resource := range []eventhub.Resource{{Kind: "global"}, {Kind: "chat", ID: "a"}, {Kind: "team"}, {Kind: "waker", ID: "a", ParentID: "b"}} {
		if validResourceShape("main-conversations", resource) {
			t.Fatalf("invalid subscription: %#v", resource)
		}
	}
}

func TestMainConversationReadingSubscriptionsUsePublicTimelineID(t *testing.T) {
	resource := eventhub.Resource{Kind: string(constants.WorkOwnerKindMainConversation), ID: "timeline"}
	for _, topic := range []string{"reading", "work-board"} {
		if !validResourceShape(topic, resource) {
			t.Fatalf("%s rejected public timeline", topic)
		}
		nested := resource
		nested.ParentID = "parent"
		if validResourceShape(topic, nested) ||
			validResourceShape(topic, eventhub.Resource{Kind: "waker", ID: "subject"}) {
			t.Fatalf("%s admitted a non-timeline resource", topic)
		}
	}
	if validResourceShape(constants.MainConversationEventTopic, resource) {
		t.Fatal("timeline ID leaked into the subject-scoped publication feed")
	}
}
