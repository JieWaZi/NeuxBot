package work_board

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPublishedLiveTargetUsesPublicConversationAndMessage(t *testing.T) {
	db := boardDatabase(t)
	old := global.MainConversationControllerManager
	global.MainConversationControllerManager = controllers.NewMainConversationController(db)
	t.Cleanup(func() { global.MainConversationControllerManager = old })
	waker := models.Waker{ID: strings.Repeat("a", 32), Name: "Report author"}
	conversation := models.MainConversation{ID: strings.Repeat("b", 32), SubjectType: "waker", SubjectID: waker.ID}
	other := models.MainConversation{ID: strings.Repeat("c", 32), SubjectType: "team", SubjectID: strings.Repeat("d", 32)}
	team := models.Channel{ID: other.SubjectID, Name: "Review team", LeadWakerID: waker.ID}
	input := models.MainConversationMessage{ID: "input", ConversationID: conversation.ID, Role: "user", Sequence: 1, Content: "Please check"}
	report := models.MainConversationMessage{ID: "report", ConversationID: conversation.ID, Role: "assistant", Sequence: 2, Content: "Ready\n for review"}
	teamReport := models.MainConversationMessage{ID: "team-report", ConversationID: other.ID, Role: "assistant", Sequence: 1, Content: "Team report"}
	for _, row := range []any{&waker, &team, &conversation, &other, &input, &report, &teamReport} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	cursor := models.MainConversationReadCursor{ReaderID: constants.LocalReaderID, ConversationID: conversation.ID, UnreadGeneration: 3}
	if err := db.Create(&cursor).Error; err != nil {
		t.Fatal(err)
	}
	lookup := func(ownerID, messageID, operation, extra string) WorkBoardLiveTarget {
		t.Helper()
		out, err := callBoard(t, &GetWorkBoardLiveTarget{}, "?workKind=main_conversation&workId="+ownerID+"&messageId="+messageID+"&operation="+operation+extra, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out.(WorkBoardLiveTarget)
	}
	target := lookup(conversation.ID, report.ID, "published", "&includePreview=true")
	if target.UnreadGeneration == nil || *target.UnreadGeneration != 3 {
		t.Fatalf("missing observed generation: %+v", target)
	}
	if !target.Valid || target.WakerID != waker.ID || target.Work == nil || target.Work.ID != conversation.ID || target.ChatID != "" || target.Preview != "Ready for review" {
		t.Fatalf("public result: %+v", target)
	}
	group := lookup(other.ID, teamReport.ID, "published", "")
	if !group.Valid || group.ChannelID != team.ID || group.WakerID != "" || group.Preview != "" {
		t.Fatalf("team result: %+v", group)
	}
	for _, invalid := range []WorkBoardLiveTarget{
		lookup(conversation.ID, input.ID, "published", ""),
		lookup(other.ID, report.ID, "published", ""),
		lookup(conversation.ID, "missing", "published", ""),
		lookup(conversation.ID, report.ID, "completed", ""),
	} {
		if invalid.Valid || invalid.Preview != "" {
			t.Fatalf("invalid target: %+v", invalid)
		}
	}
}
