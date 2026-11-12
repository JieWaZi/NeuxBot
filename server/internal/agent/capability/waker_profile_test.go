package capability

import "testing"

func TestWakerProfileCommandsFreezeTheSubmittedDraft(t *testing.T) {
	propose, ok := Find("waker.profile.propose")
	if !ok {
		t.Fatal("onboarding has no capability for proposing a profile")
	}
	fields := map[string]any{
		"expertise": []any{map[string]any{"title": "Review", "description": "Review drafts"}},
		"workStyle": []any{"Concise"}, "workMethods": []any{map[string]any{"name": "Review", "steps": []any{"Check sources"}}},
		"content": "# Working agreement", "language": "en",
	}
	if err := propose.Validate(fields); err != nil {
		t.Fatalf("complete draft rejected: %v", err)
	}
	save, ok := Find("waker.profile.save")
	if !ok || save.Validate(map[string]any{"draftId": "version"}) != nil {
		t.Fatal("saving has no capability for referring to an immutable draft")
	}
	if err := save.Validate(map[string]any{"draftId": "version", "content": "Replacement"}); err == nil {
		t.Fatal("save accepted replacement content")
	}
}

func TestWakerInterviewRequiresModelGeneratedChoiceSets(t *testing.T) {
	op, ok := Find("waker.profile.interview")
	if !ok {
		t.Fatal("onboarding has no choice interview command")
	}
	choices := []any{map[string]any{"id": "summarize", "label": "Summarize"}, map[string]any{"id": "review", "label": "Review"}}
	questions := []any{map[string]any{"id": "role", "title": "What should I own?", "options": choices},
		map[string]any{"id": "style", "title": "How should I help?", "options": choices}}
	if err := op.Validate(map[string]any{"message": "Setup", "questions": questions}); err != nil {
		t.Fatalf("complete choices rejected: %v", err)
	}
	questions[0].(map[string]any)["options"] = []any{}
	if err := op.Validate(map[string]any{"message": "Setup", "questions": questions}); err == nil {
		t.Fatal("a free-text-only question passed the choice interview contract")
	}
}
