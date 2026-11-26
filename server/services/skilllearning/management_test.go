package skilllearning

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestSkillManagementPersistsImmutableEditsAndReceipts(t *testing.T) {
	svc, db := testLibrary(t)
	svc.commands = controllers.NewAgentCommandController(db.DB)
	ctx := t.Context()
	var approvals []string
	bindings := svc.ManagementBindings("owner", "run", func(_ context.Context, reason string) error {
		approvals = append(approvals, reason)
		return nil
	})
	call := func(action, requestID string, params map[string]any) (capability.Result, error) {
		t.Helper()
		operation := "skills.packages." + action
		match := slices.IndexFunc(bindings, func(binding capability.Binding) bool { return binding.Operation == operation })
		if match < 0 {
			t.Fatalf("%s missing from bindings", operation)
		}
		return bindings[match].Call(ctx, capability.Request{Operation: operation, RequestID: requestID, Params: params})
	}
	createParams := map[string]any{"name": "checklist", "description": "Existing instructions", "content": originalMarkdown}
	created, err := call("create", "create", createParams)
	if err != nil || created.CommandID == "" {
		t.Fatalf("create result = %+v, %v", created, err)
	}
	id := created.Data.(map[string]any)["skillId"].(string)
	firstID := created.Data.(map[string]any)["versionId"].(string)
	retry, err := call("create", "create", createParams)
	if err != nil || !retry.Replayed || retry.CommandID != created.CommandID {
		t.Fatalf("create replay = %+v, %v", retry, err)
	}
	createParams["description"] = "Changed input"
	if _, err := call("create", "create", createParams); !errors.Is(err, status_error.AgentCommandRequestConflict) {
		t.Fatalf("changed request replay = %v", err)
	}
	if err := svc.skills.EnsureWakerSkill(ctx, models.WakerSkill{ID: "shared", WakerID: "other", SkillPackageID: id}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.skills.GetVersion(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	replacement := "---\nname: checklist\ndescription: Revised instructions\n---\nEntirely revised checklist.\n"
	params := map[string]any{"skillId": id, "expectedVersionId": firstID, "content": replacement}
	revised, err := call("revise", "revise", params)
	if err != nil || revised.CommandID == "" || len(approvals) != 1 || !strings.Contains(approvals[0], "Other") {
		t.Fatalf("shared revision result = %+v, %v, approvals %v", revised, err, approvals)
	}
	id2 := revised.Data.(map[string]any)["versionId"].(string)
	if id2 == firstID || revised.Data.(map[string]any)["version"] != "1.0.1" {
		t.Fatalf("revision has invalid version: %+v", revised.Data)
	}
	body, err := os.ReadFile(filepath.Join(svc.dataDirectory, first.ContentPath, "SKILL.md"))
	if err != nil || !strings.Contains(string(body), originalBody) {
		t.Fatalf("original version overwritten: %q, %v", body, err)
	}
	got, err := call("get", "", map[string]any{"skillId": id})
	if err != nil || !strings.Contains(got.Data.(map[string]any)["content"].(string), "Entirely revised checklist.") {
		t.Fatalf("published content = %+v, %v", got, err)
	}
	if _, err := call("revise", "stale", params); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("old version accepted: %v", err)
	}
	deleted, err := call("delete", "delete", map[string]any{"skillId": id, "expectedVersionId": id2})
	if err != nil || deleted.CommandID == "" || len(approvals) != 2 {
		t.Fatalf("delete result = %+v, %v, approvals %v", deleted, err, approvals)
	}
	if _, err := svc.skills.GetPackage(ctx, id); !util.HasStatusCode(err, status_error.SkillNotFound) {
		t.Fatalf("deleted skill retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(svc.dataDirectory, first.ContentPath)); !os.IsNotExist(err) {
		t.Fatalf("deleted version retained: %v", err)
	}
}

func TestSkillManagementChecksVisibilityAndRevalidatesApproval(t *testing.T) {
	svc, db := testLibrary(t)
	svc.commands = controllers.NewAgentCommandController(db.DB)
	ctx := t.Context()
	version := seedSkill(t, svc, "imported", "checklist", constants.SkillSourceZip)
	git := seedSkill(t, svc, "git", "git-check", constants.SkillSourceGit)
	createBinding := func(owner, action string) capability.Binding {
		t.Helper()
		bindings := svc.ManagementBindings(owner, "run-"+owner, func(context.Context, string) error {
			return svc.skills.EnsureWakerSkill(ctx, models.WakerSkill{
				ID: "new-sharing", WakerID: "other", SkillPackageID: "imported",
			})
		})
		operation := "skills.packages." + action
		return bindings[slices.IndexFunc(bindings, func(binding capability.Binding) bool { return binding.Operation == operation })]
	}
	deleteBinding := createBinding("owner", "delete")
	for _, target := range []models.SkillVersion{version, git} {
		request := capability.Request{Operation: deleteBinding.Operation, RequestID: target.ID, Params: map[string]any{
			"skillId": target.SkillPackageID, "expectedVersionId": target.ID,
		}}
		_, err := deleteBinding.Call(ctx, request)
		if target.ID == version.ID && !errors.Is(err, status_error.Conflict) {
			t.Fatalf("approved scope changed without invalidation: %v", err)
		}
		if target.ID == git.ID && !errors.Is(err, status_error.BadRequest) {
			t.Fatalf("Git skill was editable: %v", err)
		}
	}
	get := createBinding("third", "get")
	if _, err := get.Call(ctx, capability.Request{Operation: get.Operation, Params: map[string]any{"skillId": "imported"}}); !util.HasStatusCode(err, status_error.SkillNotFound) {
		t.Fatalf("foreign skill was readable: %v", err)
	}
	remove := createBinding("owner", "uninstall")
	if _, err := remove.Call(ctx, capability.Request{Operation: remove.Operation, RequestID: "detach", Params: map[string]any{"skillId": "imported"}}); err != nil {
		t.Fatal(err)
	}
	if partners, err := svc.skills.InstalledPartners(ctx, "imported"); err != nil || len(partners) != 1 || partners[0].ID != "other" {
		t.Fatalf("unexpected intact installation scope = %+v, %v", partners, err)
	}
	if _, err := svc.skills.GetVersion(ctx, version.ID); err != nil {
		t.Fatalf("uninstall removed the canonical version: %v", err)
	}
}

func TestSkillManagementRevisionPreservesVerifiedPackageResources(t *testing.T) {
	svc, db := testLibrary(t)
	svc.commands = controllers.NewAgentCommandController(db.DB)
	original := seedSkill(t, svc, "imported", "checklist", constants.SkillSourceZip)
	bindings := svc.ManagementBindings("owner", "run", nil)
	operation := "skills.packages.revise"
	revise := bindings[slices.IndexFunc(bindings, func(binding capability.Binding) bool { return binding.Operation == operation })]
	request := capability.Request{Operation: operation, RequestID: "revise-imported", Params: map[string]any{
		"skillId": original.SkillPackageID, "expectedVersionId": original.ID,
		"content": "---\nname: checklist\ndescription: Rewritten for the user\nmetadata:\n  license: replaced\n---\nUser-approved checklist.\n",
	}}
	ctx := t.Context()
	result, err := revise.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.skills.GetVersion(ctx, result.Data.(map[string]any)["versionId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(svc.dataDirectory, version.ContentPath)
	content, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil || !strings.Contains(string(content), "license: custom") || !strings.Contains(string(content), "User-approved checklist.") {
		t.Fatalf("existing metadata or revised content missing: %q %v", content, err)
	}
	asset := filepath.Join(root, "assets", "sample.bin")
	assetContent, err := os.ReadFile(asset)
	if err != nil || !slices.Equal(assetContent, []byte{0, 12, 255}) {
		t.Fatalf("existing package resources changed: %v, %v", assetContent, err)
	}
	if err := os.WriteFile(asset, []byte("out-of-band change"), 0o600); err != nil {
		t.Fatal(err)
	}
	request.RequestID = "revise-tampered"
	request.Params["expectedVersionId"] = version.ID
	request.Params["content"] = strings.ReplaceAll(request.Params["content"].(string), "User-approved checklist.", "Revised again.")
	if _, err := revise.Call(ctx, request); !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("out-of-band changes could be republished by a Markdown edit: %v", err)
	}
}
