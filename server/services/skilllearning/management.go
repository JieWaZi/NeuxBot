package skilllearning

import (
	"context"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const managementPrefix = "skills.packages."
const maxManagedSkillList = 50

// managementInput 定义聊天里可提交的技能编辑参数。
type managementInput struct {
	// SkillID 是已安装技能的稳定标识。
	SkillID string `json:"skillId"`
	// ExpectedVersionID 是智能体编辑前读到的版本标识。
	ExpectedVersionID string `json:"expectedVersionId"`
	// Name 是创建时指定的声明名。
	Name string `json:"name"`
	// Description 是创建时指定的适用范围。
	Description string `json:"description"`
	// Content 是完整的 SKILL.md 草稿。
	Content string `json:"content"`
}

// ManagementBindings 从宿主当前执行生成伙伴范围内的技能命令。
func (s *Service) ManagementBindings(wakerID, executionID string, approve func(context.Context, string) error, changed func(context.Context, string, string, string, string) error) []capability.Binding {
	if s == nil || s.commands == nil || wakerID == "" || executionID == "" {
		return nil
	}
	bindings := []capability.Binding{
		{Operation: managementPrefix + "list", Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			query, _ := request.Params["query"].(string)
			return s.listManagedSkills(ctx, wakerID, query)
		}},
		{Operation: managementPrefix + "get", Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			id, _ := request.Params["skillId"].(string)
			return s.readManagedSkill(ctx, wakerID, id)
		}},
	}
	for _, action := range []string{"create", "revise", "uninstall", "delete"} {
		operation := managementPrefix + action
		bindings = append(bindings, capability.Binding{
			Operation: operation,
			Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
				var input managementInput
				if err := capability.DecodeParams(request.Params, &input); err != nil {
					return capability.Result{}, err
				}
				result, err := capability.Execute(ctx, s.commands, executionID, request, func(ctx context.Context) error {
					return s.applyManagement(ctx, wakerID, action, input, approve)
				})
				if err == nil {
					notifySkillManagement(ctx, changed, action, result)
				}
				return result, err
			},
			Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
				result, err := capability.Lookup(ctx, s.commands, executionID, operation, requestID)
				if err == nil {
					notifySkillManagement(ctx, changed, action, result)
				}
				return result, err
			},
		})
	}
	return bindings
}

func (s *Service) listManagedSkills(ctx context.Context, wakerID, query string) (capability.Result, error) {
	items, err := s.skills.ListInstalledWindow(ctx, wakerID, query, maxManagedSkillList+1)
	if err != nil {
		return capability.Result{}, err
	}
	limit := min(len(items), maxManagedSkillList)
	summaries := make([]map[string]any, 0, limit)
	for _, item := range items[:limit] {
		summaries = append(summaries, managedSkillSummary(item))
	}
	return capability.Result{Data: map[string]any{"items": summaries, "truncated": len(items) > limit}}, nil
}

func (s *Service) readManagedSkill(ctx context.Context, wakerID, skillID string) (capability.Result, error) {
	item, err := s.skills.GetInstalled(ctx, wakerID, skillID)
	if err != nil {
		return capability.Result{}, err
	}
	directory, err := s.versionDirectory(item.CurrentVersion.ContentPath)
	if err != nil {
		return capability.Result{}, err
	}
	content, err := skills.ReadVersionMarkdown(directory)
	if err != nil {
		return capability.Result{}, err
	}
	summary := managedSkillSummary(item)
	summary["content"] = string(content)
	return capability.Result{Data: summary}, nil
}

func managedSkillSummary(item models.SkillWithState) map[string]any {
	summary := skillVersionResult(item.SkillPackage.ID, item.CurrentVersion)
	summary["description"] = item.CurrentVersion.Description
	summary["enabled"] = item.Enabled
	summary["revisable"] = !item.SkillPackage.BuiltIn && item.SkillPackage.Source.RevisableByLearning()
	return summary
}

func skillVersionResult(skillID string, version models.SkillVersion) map[string]any {
	return map[string]any{"skillId": skillID, "versionId": version.ID, "version": version.Version, "name": version.Name}
}

func (s *Service) applyManagement(ctx context.Context, wakerID, action string, input managementInput, approve func(context.Context, string) error) error {
	switch action {
	case "create":
		if strings.TrimSpace(input.Content) == "" || strings.TrimSpace(input.Description) == "" {
			return status_error.AgentInputInvalid
		}
		return s.publishManagedVersion(ctx, Publication{
			WakerID: wakerID, Name: util.NormalizeName(input.Name),
			directMarkdown: input.Content, directDescription: strings.TrimSpace(input.Description),
		})
	case "uninstall":
		return s.Remove(ctx, Removal{WakerID: wakerID, SkillID: input.SkillID, ExpectedVersionID: input.ExpectedVersionID})
	case "revise", "delete":
		return s.applyCurrentVersionManagement(ctx, wakerID, action, input, approve)
	default:
		return status_error.AgentOperationForbidden
	}
}

func (s *Service) applyCurrentVersionManagement(ctx context.Context, wakerID, action string, input managementInput, approve func(context.Context, string) error) error {
	if input.SkillID == "" || input.ExpectedVersionID == "" || (action == "revise" && strings.TrimSpace(input.Content) == "") {
		return status_error.AgentInputInvalid
	}
	var current models.SkillVersion
	var partners []controllers.SkillInstalledPartner
	if err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		var err error
		_, current, partners, err = s.revisionState(ctx, wakerID, input.SkillID, input.ExpectedVersionID)
		return err
	}); err != nil {
		return err
	}
	partnerIDs := make([]string, 0, len(partners))
	partnerNames := make([]string, 0, len(partners))
	for _, partner := range partners {
		partnerIDs = append(partnerIDs, partner.ID)
		partnerNames = append(partnerNames, fmt.Sprintf("%q", partner.Name))
	}
	if action == "delete" || len(partners) > 1 {
		if approve == nil {
			return status_error.AgentOperationForbidden
		}
		verb := "Publish a new version of"
		if action == "delete" {
			verb = "Delete all library versions and installations of"
		}
		reason := fmt.Sprintf("%s skill %q at version %s. Affected partners: %s.", verb, current.Name, current.Version, strings.Join(partnerNames, ", "))
		if err := approve(ctx, reason); err != nil {
			return err
		}
	}
	if action == "delete" {
		return s.Remove(ctx, Removal{
			WakerID: wakerID, SkillID: input.SkillID, DeletePackage: true,
			ExpectedVersionID: input.ExpectedVersionID, ConfirmedPartnerIDs: partnerIDs,
		})
	}
	return s.publishManagedVersion(ctx, Publication{
		WakerID: wakerID, SkillID: input.SkillID, ExpectedVersionID: input.ExpectedVersionID, Name: current.Name,
		ConfirmedPartnerIDs: partnerIDs, directMarkdown: input.Content,
	})
}

func (s *Service) publishManagedVersion(ctx context.Context, input Publication) error {
	version, err := s.prepareVersion(ctx, input)
	if err != nil {
		return err
	}
	err = s.transactions.Transaction(ctx, func(ctx context.Context) error {
		if err := s.writePublication(ctx, input, version); err != nil {
			return err
		}
		if err := capability.Commit(ctx, skillVersionResult(version.SkillPackageID, version)); err != nil {
			return err
		}
		s.emitResourceChange(ctx, version.SkillPackageID)
		return nil
	})
	if err != nil {
		if removeErr := skills.RemoveVersion(s.dataDirectory, version.ContentPath); removeErr != nil {
			logx.Errorf("remove unpublished managed skill: skill_id=%s version_id=%s error=%v", version.SkillPackageID, version.ID, removeErr)
		}
	}
	return err
}

func notifySkillManagement(ctx context.Context, changed func(context.Context, string, string, string, string) error, action string, result capability.Result) {
	if changed == nil {
		return
	}
	data, _ := result.Data.(map[string]any)
	skillID, _ := data["skillId"].(string)
	versionID, _ := data["versionId"].(string)
	name, _ := data["name"].(string)
	if err := changed(ctx, action, skillID, versionID, name); err != nil {
		logx.Warnf("publish skill management conversation event: action=%s skill_id=%s version_id=%s error=%v", action, skillID, versionID, err)
	}
}
