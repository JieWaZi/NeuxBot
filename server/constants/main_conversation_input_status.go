package constants

//go:generate toolx gen enum MainConversationInputStatus

// MainConversationInputStatus describes the handoff from the durable mailbox to a source Chat.
type MainConversationInputStatus string

const (
	// MainConversationInputQueued records a durably accepted input waiting for its source Chat.
	MainConversationInputQueued MainConversationInputStatus = "queued"
	// MainConversationInputDispatching records a reserved FIFO head.
	MainConversationInputDispatching MainConversationInputStatus = "dispatching"
	// MainConversationInputAccepted records a matching authoritative Chat receipt.
	MainConversationInputAccepted MainConversationInputStatus = "accepted"
	// MainConversationInputFailed records a definite rejection before Chat admission.
	MainConversationInputFailed MainConversationInputStatus = "failed"
	// MainConversationInputUncertain holds a source until the previous admission can be reconciled.
	MainConversationInputUncertain MainConversationInputStatus = "uncertain"
)
