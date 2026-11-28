package events

import (
	"testing"

	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
)

func TestWorkTicketSubscriptionsRequireExactConversation(t *testing.T) {
	for _, kind := range []string{"chat", "main"} {
		command := `{"type":"subscribe","requestId":"work-tickets-test","topic":"work-tickets","resource":{"kind":"` + kind + `","id":"conversation"}}`
		if _, err := decodeControl([]byte(command)); err != nil {
			t.Fatalf("subscribe to %s ticket updates: %v", kind, err)
		}
	}
	for _, resource := range []eventhub.Resource{
		{Kind: "global"},
		{Kind: "team", ID: "conversation"},
		{Kind: "main"},
		{Kind: "chat", ID: "conversation", ParentID: "other"},
	} {
		if validResourceShape("work-tickets", resource) {
			t.Errorf("accepted invalid work ticket resource: %+v", resource)
		}
	}
}
