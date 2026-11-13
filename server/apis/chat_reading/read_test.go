package chat_reading

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestReadingAPIIdentityAndGeneration(t *testing.T) {
	for _, raw := range []string{
		`{"throughMessageID":"shown","observedUnreadGeneration":0,"intent":"explicit","readerId":"another-reader"}`,
		`{"throughMessageID":"shown","intent":"explicit"}`,
		`{"throughMessageID":"shown","observedUnreadGeneration":null,"intent":"explicit"}`,
	} {
		var value MarkChatReadRequest
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			t.Fatalf("invalid command accepted: %s", raw)
		}
	}
	var value MarkChatReadRequest
	if err := json.Unmarshal([]byte(`{"throughMessageID":"shown","observedUnreadGeneration":0,"intent":"explicit"}`), &value); err != nil || value.Intent != constants.ReadingIntentExplicit || value.ThroughMessageID != "shown" {
		t.Fatalf("valid command: %+v %v", value, err)
	}
}
