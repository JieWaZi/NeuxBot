package agent

import (
	"encoding/json"
	"slices"

	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	native "github.com/JieWaZi/acp-go/pkg/agentclient"
)

// CoreMemoryContext carries one complete version of a partner's selected memories.
type CoreMemoryContext struct {
	// Revision identifies a distinct durable snapshot without belonging to static settings.
	Revision uint64
	// JSON contains the bounded, serialized reference data.
	JSON []byte
}

type coreMemoryDelivery struct {
	// session is the exact native history address that received this revision.
	session NativeSessionKey
	// revision is the last snapshot confirmed on that history.
	revision uint64
}

func (t *Turn) promptRequest() (AgentPromptRequest, *CoreMemoryContext) {
	prompt := AgentPromptRequest{
		SessionID: t.binding.Session.SessionID, HostInvocationID: t.input.HostInvocationID,
		Text: t.input.Text, Content: t.input.Content, Context: t.input.Context, BeforeSubmit: t.beforeSubmit,
	}
	snapshot := t.input.CoreMemory
	if snapshot == nil {
		return prompt, nil
	}
	t.lifecycle.mu.Lock()
	delivered := t.thread.coreMemory
	t.lifecycle.mu.Unlock()
	if delivered.session == t.binding.Session && delivered.revision == snapshot.Revision {
		return prompt, nil
	}
	var document struct {
		// Items contains the stored snapshot; only text is relevant to the model.
		Items []struct {
			// Text is the memory body.
			Text string `json:"text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(snapshot.JSON, &document); err != nil || document.Items == nil {
		return prompt, nil
	}
	if len(document.Items) == 0 && t.freshSession {
		return prompt, snapshot
	}
	texts := make([]string, len(document.Items))
	for i, item := range document.Items {
		texts[i] = item.Text
	}
	prompt.Context = append(slices.Clone(prompt.Context), TurnFact{
		Name: "Core memories", Value: texts,
	})
	return prompt, snapshot
}

func (t *Turn) confirmCoreMemory(snapshot CoreMemoryContext, result TurnResult) {
	if !result.BusinessAdmitted || result.Admission != string(native.AdmissionConfirmed) || result.Outcome == nil ||
		result.Outcome.Turn.Status != acpmeta.NativeTurnCompleted || result.Outcome.NativeFailure != nil ||
		result.OperationError != nil || result.StorageError != nil || result.DeliveryError != nil {
		return
	}
	t.lifecycle.mu.Lock()
	t.thread.coreMemory = coreMemoryDelivery{session: t.binding.Session, revision: snapshot.Revision}
	t.lifecycle.mu.Unlock()
}
