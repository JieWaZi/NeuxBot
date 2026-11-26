package reviewstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
)

func TestStaleSkillApprovalCannotConfirmRefreshedScope(t *testing.T) {
	ctx := t.Context()
	store, worker, target, proposal := setupSkillApproval(t)
	proposal.SharedPartnerIDs = []string{"owner", "other"}
	seedSkillSuggestion(t, store, proposal)
	target.PartnerIDs = []string{"owner", "other", "third"}

	stale := proposal.ApprovalToken()
	var refreshed Suggestion
	for range 2 {
		updated, err := worker.ResolveSuggestion(ctx, proposal.ChatID, proposal.ID, DecisionSave, stale)
		if err != nil || updated.Status != StatusPending {
			t.Fatalf("stale approval resolved as %q: %v", updated.Status, err)
		}
		refreshed = updated
	}
	if refreshed.ApprovalToken() == stale || !sameScope(refreshed.SharedPartnerIDs, target.PartnerIDs) {
		t.Fatalf("stale card was not refreshed: %+v", refreshed)
	}
	for range 2 {
		queued, err := worker.ResolveSuggestion(ctx, proposal.ChatID, proposal.ID, DecisionSave, refreshed.ApprovalToken())
		if err != nil || queued.Status != StatusQueued || !queued.ConfirmedScope {
			t.Fatalf("current approval resolved as %q: %v", queued.Status, err)
		}
	}
}

func TestRevisedSkillVersionRequiresARefreshedApproval(t *testing.T) {
	ctx := t.Context()
	store, worker, target, proposal := setupSkillApproval(t)
	seedSkillSuggestion(t, store, proposal)
	target.VersionID = "v2"
	for _, token := range []string{proposal.ApprovalToken(), ""} {
		refreshed, err := worker.ResolveSuggestion(ctx, proposal.ChatID, proposal.ID, DecisionSave, token)
		if err != nil || refreshed.Status != StatusPending || refreshed.VersionID != target.VersionID {
			t.Fatalf("unconfirmed version resolved as %+v: %v", refreshed, err)
		}
	}
	ignored, err := worker.ResolveSuggestion(ctx, proposal.ChatID, proposal.ID, DecisionIgnore, "")
	if err != nil || ignored.Status != StatusIgnored {
		t.Fatalf("ignore unexpectedly required an approval token: %+v, %v", ignored, err)
	}
}

func TestQueuedRevisionRechecksTheConfirmedScopeBeforeAuthoring(t *testing.T) {
	ctx := t.Context()
	store, worker, target, proposal := setupSkillApproval(t)
	proposal.SharedPartnerIDs = []string{"owner", "other"}
	target.PartnerIDs = proposal.SharedPartnerIDs
	seedSkillSuggestion(t, store, proposal)
	queued, err := worker.ResolveSuggestion(ctx, proposal.ChatID, proposal.ID, DecisionSave, proposal.ApprovalToken())
	if err != nil || queued.Status != StatusQueued {
		t.Fatalf("did not queue confirmed suggestion: %+v, %v", queued, err)
	}
	target.PartnerIDs = []string{"owner"}
	worker.ports.Published = func(context.Context, Suggestion) (PublishedSkill, bool, error) { return PublishedSkill{}, false, nil }
	worker.save(ctx, queued)
	latest, err := store.Suggestion(ctx, proposal.ChatID, proposal.ID)
	if err != nil || latest.Status != StatusPending || len(latest.SharedPartnerIDs) != 0 || latest.ConfirmedScope {
		t.Fatalf("changed scope still carried approval: %+v, %v", latest, err)
	}
}

func TestRevisionAuthoringUsesTheOriginalEvidenceBehindTheMessageAnchor(t *testing.T) {
	ctx := t.Context()
	store, worker, target, proposal := setupSkillApproval(t)
	proposal.From, proposal.Through = 0, 2
	proposal.EvidenceIDs = []string{"tool-1", "user-2", "assistant-2"}
	proposal.SourceMessageIDs = []string{"rendered-assistant"}
	target.Markdown = "---\nname: checklist\ndescription: 检查清单\n---\n使用标准模板。\n保留每条验证结果。\n" + strings.Repeat("核", 3800)
	seedSkillSuggestion(t, store, proposal)
	if err := worker.UpdateSettings(ctx, Settings{Source: SourceGlobal, RuntimeID: "codex", Model: "new"}); err != nil {
		t.Fatal(err)
	}
	if visible, err := worker.ChatSuggestions(ctx, proposal.ChatID, 0); err != nil || len(visible) != 1 {
		t.Fatalf("suggestion disappeared after changing models: %+v, %v", visible, err)
	}
	queued, err := worker.ResolveSuggestion(ctx, proposal.ChatID, proposal.ID, DecisionSave, proposal.ApprovalToken())
	if err != nil || queued.Status != StatusQueued {
		t.Fatalf("did not queue confirmed suggestion: %+v, %v", queued, err)
	}
	worker.ports.Published = func(context.Context, Suggestion) (PublishedSkill, bool, error) { return PublishedSkill{}, false, nil }
	worker.ports.Turns = func(_ context.Context, _ string, _, _ int64, admission Admission, _ int) ([]Turn, error) {
		if admission != (Admission{Skill: proposal.Origin.Skill}) {
			t.Fatalf("skill authoring did not use the suggestion's original evidence: %+v", admission)
		}
		longText := strings.Repeat(" example", 500)
		return []Turn{
			{Sequence: 1, UserID: "user", User: "请先核对模板。" + longText, AssistantID: "rendered-assistant",
				Assistant: "已完成核对。" + longText, Skill: true,
				Steps: []ToolStep{{ID: "tool-1", Name: "confirm", Summary: "Checked the template."}}},
			{Sequence: 2, UserID: "user-2", User: "请保留报告。" + longText, AssistantID: "assistant-2",
				Assistant: "已保留报告。" + longText, Skill: true},
		}, nil
	}
	worker.ports.Generate = func(ctx context.Context, _ Selection, _, _, instruction, input string, authorize func(context.Context) error) (string, error) {
		if !strings.Contains(instruction, "language and writing style of the existing skill") || len(input) > EvidenceBytes {
			t.Fatalf("unexpected revision prompt, size %d", len(input))
		}
		for _, expected := range []string{"使用标准模板。", "Checked the template.", "请保留报告。", "已保留报告。"} {
			if !strings.Contains(input, expected) {
				t.Fatalf("required revision evidence %q was dropped", expected)
			}
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(input), &fields); err != nil || len(fields) != 3 ||
			fields["lesson"] == nil || fields["turns"] == nil || fields["currentSkill"] == nil {
			t.Fatal("skill revision should send only the lesson, evidence, and current skill")
		}
		for _, omitted := range []string{`"userRef"`, `"assistantRef"`, `"ref"`, "tool-1", "user-2", "assistant-2"} {
			if strings.Contains(input, omitted) {
				t.Fatalf("skill revision included unused data %q", omitted)
			}
		}
		if err := authorize(ctx); err != nil {
			return "", err
		}
		if err := worker.UpdateSettings(ctx, Settings{Model: "next", Source: SourceGlobal, RuntimeID: "codex"}); err != nil {
			return "", err
		}
		return `{"revision":{"edits":[{"before":"使用标准模板。","after":"核对后使用标准模板。"}]}}`, nil
	}
	worker.ports.Publish = func(_ context.Context, _ Suggestion, draft skills.LearnedDraft, _ []string, acquire func() (func(), error)) (PublishedSkill, error) {
		if draft.Markdown != "" || draft.Revision == nil || len(draft.Revision.Edits) != 1 {
			t.Fatalf("revision was not sent as a localized edit: %+v", draft)
		}
		release, err := acquire()
		if err != nil {
			return PublishedSkill{}, err
		}
		defer release()
		return PublishedSkill{SkillID: "skill", VersionID: "v2"}, nil
	}
	worker.save(ctx, queued)
	saved, err := store.Suggestion(ctx, proposal.ChatID, proposal.ID)
	if err != nil || saved.Status != StatusSaved || saved.VersionID != "v2" {
		t.Fatalf("unexpected revision result: %+v, %v", saved, err)
	}
}

func setupSkillApproval(t *testing.T) (*Store, *Worker, *SkillTarget, Suggestion) {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPartner(ctx, "owner", Partner{SkillMode: ModeAsk, SkillGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	target := SkillTarget{VersionID: "v1", Name: "checklist", PartnerIDs: []string{"owner"}}
	worker := New(store, Ports{
		Chat:  func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "owner"}, nil },
		Guard: func(string, string) (func(), error) { return func() {}, nil },
		Resolve: func(context.Context, string, Settings) (Selection, error) {
			return Selection{RuntimeID: "claude"}, nil
		},
		Target: func(_ context.Context, _, _, version string) (SkillTarget, error) {
			if version != "" && version != target.VersionID {
				return SkillTarget{}, status_error.Conflict
			}
			return target, nil
		},
	})
	origin := worker.Freeze(ctx, "owner", false, false)
	return store, worker, &target, Suggestion{
		ID: "suggestion", ChatID: "chat", WakerID: "owner", Name: "checklist", Lesson: "Verify the checklist.",
		Action: ActionUpdate, SkillID: "skill", VersionID: "v1", Status: StatusPending, Origin: *origin,
	}
}

func seedSkillSuggestion(t *testing.T, store *Store, v Suggestion) {
	t.Helper()
	ctx := t.Context()
	if err := store.Observe(ctx, v.ChatID, v.WakerID); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitOnly(ctx, func(tx *sql.Tx, _ bool) error {
		_, err := CommitProgress(ctx, tx, Progress{ChatID: v.ChatID}, 1, &v)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailedSkillSuggestionCanBeIgnoredWhenRuntimeUnavailable(t *testing.T) {
	store, worker, _, suggestion := setupSkillApproval(t)
	suggestion.Status, suggestion.Error = StatusFailed, "generationFailed"
	seedSkillSuggestion(t, store, suggestion)
	worker.ports.Resolve = func(context.Context, string, Settings) (Selection, error) {
		return Selection{}, status_error.RuntimeUnavailable
	}
	updated, err := worker.ResolveSuggestion(t.Context(), suggestion.ChatID, suggestion.ID, DecisionIgnore, "")
	if err != nil || updated.Status != StatusIgnored || updated.Error != "" {
		t.Fatalf("failed suggestion could not be ignored: %+v %v", updated, err)
	}
	stored, err := store.Suggestion(t.Context(), suggestion.ChatID, suggestion.ID)
	if err != nil || stored.Status != StatusIgnored {
		t.Fatalf("ignore did not persist: %+v %v", stored, err)
	}
}

func TestReviewSuggestionTimelineUsesPublicationTimeAndSkipsIgnored(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	items := []Suggestion{
		{ID: "old-pending", Status: StatusPending, CreatedAt: 10},
		{ID: "later-publication", Status: StatusSaved, CreatedAt: 15, PublishedAt: 100},
	}
	for i := range 55 {
		items = append(items, Suggestion{
			ID: fmt.Sprintf("ignored-%d", i), Status: StatusIgnored, CreatedAt: int64(30 + i),
		})
	}
	if err := store.Observe(ctx, "chat", "partner"); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitOnly(ctx, func(tx *sql.Tx, _ bool) error {
		for index, item := range items {
			item.ChatID, item.WakerID, item.Name = "chat", "partner", item.ID
			p := Progress{ChatID: item.ChatID, Cursor: int64(index)}
			if _, err := CommitProgress(ctx, tx, p, p.Cursor+1, &item); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	current, err := store.Suggestions(ctx, "chat", 90)
	if err != nil || len(current) != 1 || current[0].ID != "later-publication" {
		t.Fatalf("wrong current suggestions: %+v %v", current, err)
	}
	expanded, err := store.Suggestions(ctx, "chat", 5)
	if err != nil || len(expanded) != 2 || expanded[0].ID != "old-pending" || expanded[1].ID != "later-publication" {
		t.Fatalf("wrong expanded suggestions: %+v %v", expanded, err)
	}
}
