package agent

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// ConversationStore is the durable boundary shared by the runtime and Chat readers.
type ConversationStore interface {
	Commit(context.Context, ConversationBatch) (CommitReceipt, error)
	ReadVersion(context.Context, string) (int64, error)
	ReadChanges(context.Context, string, int64, int) (ChangePage, error)
	ReadMessages(context.Context, MessageQuery) (MessagePage, error)
	ReadMessage(context.Context, string, string) (StoredMessage, error)
	ReadHistoryCandidates(context.Context, MessageQuery) ([]HistoryCandidate, int64, error)
	// ReadBinding returns nil when the Chat has no usable durable binding.
	ReadBinding(context.Context, string) (*NativeBinding, error)
	ReadTurn(context.Context, string, string) ([]ConversationRecord, error)
	CommitDetachedInput(context.Context, HostInput) error
	CommitNativeFragments(context.Context, NativeFragmentBatch) (NativeFragmentRange, error)
	ReadNativeFragments(context.Context, NativeFragmentRange) ([]MessageChange, error)
	CommitNativeResult(context.Context, NativeResult) error
	ReadNativeResult(context.Context, NativeResultKey) (NativeResult, error)
	ReadNativeInputs(context.Context, NativeSessionKey, []string) ([]DurableHostInput, error)
	ConfirmNativeResult(context.Context, NativeResultKey, []HostInputRef) error
}

// ConversationBatch publishes all of its changes at one durable boundary.
type ConversationBatch struct {
	// ChatID identifies the public conversation.
	ChatID string
	// Key identifies a retry of this command within the conversation.
	Key string
	// ExpectedVersion is the last durable conversation cursor seen by the caller.
	ExpectedVersion int64
	// Changes are committed in their given order.
	Changes []ConversationChange
}

// CommitReceipt identifies the same durable range on every successful retry.
type CommitReceipt struct {
	// First is the inclusive cursor of the first change.
	First int64
	// Version is the inclusive cursor of the final change and the CAS version.
	Version int64
	// RecordedAt is the original commit time in Unix milliseconds.
	RecordedAt int64
}

// ConversationRecord is one durable change with its public cursor.
type ConversationRecord struct {
	// Cursor is the stable sequence within the Chat.
	Cursor int64
	// RecordedAt is the commit time in Unix milliseconds.
	RecordedAt int64
	// Change is the typed content at this cursor.
	Change ConversationChange
}

// ConversationChange holds exactly one semantic change.
type ConversationChange struct {
	// Fact is a Chat or Execution business event.
	Fact *ChatFact `json:"fact,omitempty"`
	// Message is a bounded change to a message.
	Message *MessageChange `json:"message,omitempty"`
	// Binding replaces or clears the native session used for future input.
	Binding *NativeBinding `json:"binding,omitempty"`
	// Input records the identity authorized at BeforeSubmit.
	Input *HostInput `json:"input,omitempty"`
	// Consumption links the admitted host input to the canonical native outcome.
	Consumption *InputConsumption `json:"consumption,omitempty"`
}

// ChatFact retains existing Chat business event identities and payload schemas.
type ChatFact struct {
	// Type identifies the existing business event.
	Type constants.ChatEventType `json:"type"`
	// MessageID is the related public message identity when applicable.
	MessageID string `json:"messageId,omitempty"`
	// ExecutionID is the related business execution identity when applicable.
	ExecutionID string `json:"executionId,omitempty"`
	// InvocationID is the related host invocation, when supplied.
	InvocationID string `json:"invocationId,omitempty"`
	// Data is the JSON payload specific to Type.
	Data json.RawMessage `json:"data,omitempty"`
}

// MessageChangeKind defines how a committed fragment folds into one message.
type MessageChangeKind string

const (
	MessageAppend         MessageChangeKind = "append"
	MessageReplace        MessageChangeKind = "replace"
	MessageToolSnapshot   MessageChangeKind = "tool_snapshot"
	MessageComplete       MessageChangeKind = "complete"
	MessageNativeFinal    MessageChangeKind = "native_final"
	MessageNativeFragment MessageChangeKind = "native_fragment"
	MessageEvent          MessageChangeKind = "event"
)

// MessageChange stores a bounded fragment, complete replacement, or canonical reference.
type MessageChange struct {
	// MessageID is stable from the first fragment through completion.
	MessageID string `json:"messageId"`
	// ExecutionID identifies the business execution that owns the message.
	ExecutionID string `json:"executionId,omitempty"`
	// Kind selects append, replacement, tool snapshot, completion or canonical final text.
	Kind MessageChangeKind `json:"kind"`
	// Text is the append fragment or complete replacement.
	Text string `json:"text,omitempty"`
	// ContentType preserves the display semantics of text, commentary, reasoning and tool changes.
	ContentType constants.RuntimeEventType `json:"contentType,omitempty"`
	// Revision is the producer-assigned message revision after this change.
	Revision int64 `json:"revision,omitempty"`
	// ToolCallID is the identity of the tool snapshot being replaced.
	ToolCallID string `json:"toolCallId,omitempty"`
	// Data is a complete tool snapshot for ToolCallID.
	Data json.RawMessage `json:"data,omitempty"`
	// Result references final text in the native store without copying it into Chat.
	Result *NativeResultKey `json:"result,omitempty"`
	// Fragments references one committed native message change batch.
	Fragments *NativeFragmentRange `json:"fragments,omitempty"`
}

// MessageQuery reads the latest page or the page before an earlier message start.
type MessageQuery struct {
	// ChatID identifies the conversation.
	ChatID string
	// Before is an exclusive message-start cursor, or zero for the latest page.
	Before int64
	// Limit bounds the number of messages returned.
	Limit int
}

// MessagePage contains messages ordered by their first durable cursor.
type MessagePage struct {
	// Messages is the selected chronological page.
	Messages []StoredMessage
	// Before is the next older page anchor, or zero at the beginning.
	Before int64
	// Version is the latest durable change visible to this read.
	Version int64
}

// StoredMessage folds durable message changes for Chat and bounded history reads.
type StoredMessage struct {
	// ID is the stable public message identity.
	ID string
	// Cursor is the first durable change for this message.
	Cursor int64
	// ExecutionID is the owning business execution when known.
	ExecutionID string
	// Role distinguishes user and assistant history.
	Role constants.MessageRole
	// Text is the folded assistant or user body.
	Text string
	// Parts preserves effective ordered runtime content with its normalized event semantics.
	Parts []MessagePart
	// Revision is the last durable producer-assigned message revision.
	Revision int64
	// Complete reports a durable completion marker.
	Complete bool
	// Legacy preserves old projection facts and their original identities where applicable.
	Legacy []ChatFact
}

// MessagePart is one effective ordered segment of a stored message.
type MessagePart struct {
	// Type distinguishes assistant text, commentary, thinking and tool events.
	Type constants.RuntimeEventType
	// Text is the content of a text segment.
	Text string
	// ToolCallID is the identity of an affected tool, when applicable.
	ToolCallID string
	// Data is the normalized payload of a non-text segment or snapshot.
	Data json.RawMessage
}

// HistoryCandidate is a completed turn pair or a single inherited message.
type HistoryCandidate struct {
	// Cursor is the original turn or inherited message start.
	Cursor int64
	// UserMessageID identifies the admitted user input when known.
	UserMessageID string
	// MessageID is the completed assistant or inherited message identity.
	MessageID string
	// Inherited identifies imported display history without a new execution.
	Inherited bool
}

// ChangePage reads forward from an exclusive durable cursor for replay.
type ChangePage struct {
	// Records contains the selected committed changes in durable order.
	Records []ConversationRecord
	// Next is the last returned cursor, or the requested cursor for an empty page.
	Next int64
	// Version is the latest durable change visible to this read.
	Version int64
	// More reports whether another page follows this one.
	More bool
}

// NativeSessionKey identifies a native session within its frozen authentication subject.
type NativeSessionKey struct {
	// Provider is the stable native provider identity.
	Provider string `json:"provider"`
	// Subject is the frozen account subject identifier without credentials.
	Subject string `json:"subject"`
	// SessionID is the actual native session identity.
	SessionID string `json:"sessionId"`
}

// NativeBinding is the last durable session binding for a conversation.
type NativeBinding struct {
	// Session identifies the native session that was actually opened.
	Session NativeSessionKey `json:"session"`
	// Cleared invalidates this exact binding.
	Cleared bool `json:"cleared,omitempty"`
	// Configuration contains the safe effective settings and compatibility evidence.
	Configuration json.RawMessage `json:"configuration,omitempty"`
}

// HostInput captures identity before submission without asserting native acceptance.
type HostInput struct {
	// HostInvocationID identifies the host invocation H.
	HostInvocationID string `json:"hostInvocationId"`
	// AdapterInputID is the input identity used by native outcome consumption.
	AdapterInputID string `json:"adapterInputId"`
	// NativeInputID is the optional wire identity actually supported by the provider.
	NativeInputID string `json:"nativeInputId,omitempty"`
	// Session is the frozen destination of the input.
	Session NativeSessionKey `json:"session"`
	// ExecutionID identifies the related public execution, absent for private text work.
	ExecutionID string `json:"executionId,omitempty"`
	// AssistantMessageID is the durable reply identity used when the result outlives the waiter.
	AssistantMessageID string `json:"assistantMessageId,omitempty"`
}

// InputConsumption links H to the one native invocation P that consumed its adapter input.
type InputConsumption struct {
	// HostInvocationID identifies the already admitted input within the Chat.
	HostInvocationID string `json:"hostInvocationId"`
	// Result is the canonical native result address.
	Result NativeResultKey `json:"result"`
}

// HostInputRef identifies the durable Chat relation required to ACK native consumption.
type HostInputRef struct {
	// ChatID locates the public admission and relation. It is empty for a detached admission.
	ChatID string
	// HostInvocationID identifies the admitted host invocation.
	HostInvocationID string
}

// DurableHostInput links an adapter-consumed identity to its original admission.
type DurableHostInput struct {
	// Ref locates the admission's public or detached owner.
	Ref HostInputRef
	// Input is the durable frozen BeforeSubmit fact.
	Input HostInput
}

// NativeResultKey is independent of foreground and receipt delivery order.
type NativeResultKey struct {
	// Session scopes the invocation to its frozen provider, subject and session.
	Session NativeSessionKey `json:"session"`
	// InvocationID identifies the native invocation P within that session.
	InvocationID string `json:"invocationId"`
}

// NativeFinalMessage is provider-proven complete output.
type NativeFinalMessage struct {
	// ID is the stable native message identity used for reconciliation.
	ID string `json:"id"`
	// Text is the complete final answer, including a known empty answer.
	Text string `json:"text"`
}

// NativeResult is the immutable terminal fact. It does not record waiter or delivery failures.
type NativeResult struct {
	// Key identifies the unique durable native outcome.
	Key NativeResultKey `json:"key"`
	// Turn carries terminal status, exact adapter inputs, primary usage and overlapping observations.
	Turn acpmeta.NativeTurn `json:"turn"`
	// FinalMessage is the optional native-proven final body.
	FinalMessage *NativeFinalMessage `json:"finalMessage,omitempty"`
	// StopReason is the native terminal classification when known.
	StopReason string `json:"stopReason,omitempty"`
	// ForkPosition is a provider-confirmed exact resume or fork position.
	ForkPosition string `json:"forkPosition,omitempty"`
	// Failure is the safe native failure classification, without transport or delivery errors.
	Failure *acpmeta.NativeTurnFailure `json:"failure,omitempty"`
	// Unavailable identifies fields the adapter could not retain.
	Unavailable []string `json:"unavailable,omitempty"`
}

// NativeFragmentBatch persists a bounded set of ordered changes for one native message.
type NativeFragmentBatch struct {
	// Key identifies the invocation independently of the Chat.
	Key NativeResultKey
	// CommitKey identifies a retry of this fragment batch.
	CommitKey string
	// MessageID is the provider message identity used for final reconciliation.
	MessageID string
	// Changes contains normalized append, replace and tool snapshot operations.
	Changes []MessageChange
}

// NativeFragmentRange refers to one durable native fragment batch.
type NativeFragmentRange struct {
	// Key scopes the references to one invocation.
	Key NativeResultKey `json:"key"`
	// MessageID is the owning native message identity.
	MessageID string `json:"messageId"`
	// First is the inclusive native stream cursor.
	First int64 `json:"first"`
	// Last is the inclusive native stream cursor.
	Last int64 `json:"last"`
}
