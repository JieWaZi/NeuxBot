package capability

import (
	"maps"
	"strings"
	"testing"
)

func TestWakerProfileEditAcceptsOnlyCurrentPartnerFields(t *testing.T) {
	get, ok := Find("waker.profile.get")
	if !ok || get.Validate(map[string]any{}) != nil || get.Validate(map[string]any{"wakerId": "other"}) == nil {
		t.Fatal("profile read must take its partner identity from the chat")
	}
	edit, ok := Find("waker.profile.optimize")
	if !ok {
		t.Fatal("profile edit capability is missing")
	}
	revision := strings.Repeat("a", 64)
	for _, input := range []map[string]any{
		{"revision": revision, "content": "Follow `研发手册.md`."},
		{"revision": revision, "workStyle": []any{}},
	} {
		if err := edit.Validate(input); err != nil {
			t.Fatalf("requested field changes rejected: %v", err)
		}
	}
	for _, input := range []map[string]any{
		{"content": "# Revision"},
		{"revision": revision},
		{"revision": revision, "content": "# Revision", "wakerId": "other"},
	} {
		if edit.Validate(input) == nil {
			t.Fatalf("invalid profile change accepted: %v", input)
		}
	}
}

func TestWakerProfileSaveAcceptsStructuredOverviewAndInstructions(t *testing.T) {
	save, ok := Find("waker.profile.save")
	if !ok {
		t.Fatal("onboarding has no capability for saving a profile")
	}
	fields := map[string]any{
		"roleName":  "文档评审员",
		"bio":       "检查主要结论和证据。",
		"expertise": []any{map[string]any{"title": "📝 评审 · 复核结论", "description": "检查主要结论和证据。"}},
		"workStyle": []any{"结论优先"}, "workMethods": []any{map[string]any{"name": "文档评审", "steps": []any{"核对来源"}}},
		"content": "# Working agreement",
	}
	if err := save.Validate(fields); err != nil {
		t.Fatalf("complete profile rejected: %v", err)
	}
	for _, field := range []string{"roleName", "bio"} {
		incomplete := maps.Clone(fields)
		delete(incomplete, field)
		if err := save.Validate(incomplete); err == nil {
			t.Fatalf("save accepted a profile missing %s", field)
		}
	}
	if err := save.Validate(map[string]any{"draftId": "version"}); err == nil {
		t.Fatal("save accepted a legacy confirmation draft ID")
	}
	if _, ok := Find("waker.profile.propose"); ok {
		t.Fatal("onboarding still exposes the confirmation capability")
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
