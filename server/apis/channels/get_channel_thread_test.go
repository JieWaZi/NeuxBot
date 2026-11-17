package channels

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestThreadDelegationsPreserveOriginalRecipientAndRetrySender(t *testing.T) {
	first, retry := int64(100), int64(200)
	got := threadDelegations(models.ChannelThread{LeadWakerID: "lead"}, []models.ChannelTask{
		{ID: "delegated", SenderWakerID: "lead", AssigneeWakerID: "replacement", CreatedAt: 50},
		{ID: "user-task", AssigneeWakerID: "replacement"},
	}, []models.ChannelTaskAttempt{
		{ID: "first", TaskID: "delegated", ExecutionID: "member-run", AssigneeWakerID: "original", Reason: constants.ChannelAttemptReasonInitial, DelegationSentAt: 50, CreatedAt: 80, StartedAt: &first},
		{ID: "retry", TaskID: "delegated", AssigneeWakerID: "replacement", Reason: constants.ChannelAttemptReasonRetry, StartedAt: &retry},
		{ID: "user-first", TaskID: "user-task", AssigneeWakerID: "original", Reason: constants.ChannelAttemptReasonInitial, StartedAt: &first},
		{ID: "user-retry", TaskID: "user-task", AssigneeWakerID: "replacement", Reason: constants.ChannelAttemptReasonRetry, StartedAt: &retry},
		{ID: "user-reassigned", TaskID: "user-task", AssigneeWakerID: "replacement", Reason: constants.ChannelAttemptReasonReassigned, StartedAt: &retry},
	})
	if len(got) != 4 || got[0].AssigneeWakerID != "original" || got[0].CreatedAt != first ||
		got[1].AssigneeWakerID != "replacement" || got[1].CreatedAt != retry ||
		got[2].SenderWakerID != "lead" || got[2].ID != "user-retry" || got[3].SenderWakerID != "lead" {
		t.Fatalf("delegation history = %+v", got)
	}
	if got[0].ExecutionID != "member-run" || got[0].QueuedAt != 80 || got[0].SentAt != 50 {
		t.Fatalf("delegation lost original message anchors: %+v", got[0])
	}
}

func TestThreadDelegationsKeepRequestTimeBeforeDelayedDispatch(t *testing.T) {
	for _, reason := range []constants.ChannelAttemptReason{constants.ChannelAttemptReasonRetry, constants.ChannelAttemptReasonReassigned} {
		t.Run(string(reason), func(t *testing.T) {
			acceptedAt := int64(600)
			got := threadDelegations(models.ChannelThread{LeadWakerID: "lead"}, []models.ChannelTask{{
				ID: "task", SenderWakerID: "original-sender", CreatedAt: 50, UpdatedAt: 900,
			}}, []models.ChannelTaskAttempt{{
				ID: "attempt", TaskID: "task", AssigneeWakerID: "member", Reason: reason,
				DelegationSentAt: 100, CreatedAt: 500, StartedAt: &acceptedAt,
			}})
			if len(got) != 1 || got[0].SentAt != 100 || got[0].QueuedAt != 500 || got[0].CreatedAt != 600 || got[0].SenderWakerID != "lead" {
				t.Fatalf("delayed delegation changed request identity: %+v", got)
			}
		})
	}
}
