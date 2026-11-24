package skilllearning

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Config 定义技能学习在技能库内使用的依赖。
type Config struct {
	// DataDirectory 是持有不可变技能版本的应用数据目录。
	DataDirectory string
	// Skills 提供已有技能库及启停关系。
	Skills controllers.SkillController
	// Wakers 用于在安装时核对来源伙伴仍存在。
	Wakers controllers.WakerController
	// Plugins 提供固定插件版本的有效技能声明。
	Plugins controllers.PluginController
	// Transactions 保证版本、关联和回执原子提交。
	Transactions controllers.TransactionController
	// Events 在发布完成后通知资源消费者。
	Events events.Publisher
	// Commands 在显式技能管理与业务事实的同一事务中保存命令回执。
	Commands capability.Store
}

// Service 供自动保存与用户确认入口查询并发布技能学习结果。
type Service struct {
	// dataDirectory 是持有不可变技能版本的应用数据目录。
	dataDirectory string
	// skills 提供已有技能库及启停关系。
	skills controllers.SkillController
	// wakers 用于在安装时核对来源伙伴仍存在。
	wakers controllers.WakerController
	// plugins 提供固定插件版本的有效技能声明。
	plugins controllers.PluginController
	// transactions 保证版本、关联和回执原子提交。
	transactions controllers.TransactionController
	// events 在发布完成后通知资源消费者。
	events events.Publisher
	// commands 持有聊天命令回执。
	commands capability.Store
}

// Candidate 描述来源伙伴当前可用的技能声明及其修订资格。
type Candidate struct {
	// SkillID 是独立技能或插件的资源标识。
	SkillID string
	// VersionID 是当前技能版本或固定插件版本的标识。
	VersionID string
	// Name 是运行时读取的技能声明名。
	Name string
	// Description 是发布版本的技能用途。
	Description string
	// Revisable 表示可作为技能学习的修订目标。
	Revisable bool
}

// Target 描述拟修订的不可变本地版本和受影响的安装范围。
type Target struct {
	// Candidate 是现有技能身份与用途。
	Candidate
	// Markdown 是受管版本的有界 SKILL.md 原文，发送前由调用者执行敏感内容检查。
	Markdown string
	// AffectedPartners 是完整安装范围，包含停用关系。
	AffectedPartners []controllers.SkillInstalledPartner
}

// Publication 是复盘服务授权后提交的学习技能内容。
type Publication struct {
	// SuggestionID 是同一学习建议的唯一命令身份。
	SuggestionID string
	// WakerID 标识来源伙伴。
	WakerID string
	// Name 是建议引用的技能声明名。
	Name string
	// Draft 是新建文档或对不可变原文的局部修订。
	Draft skills.LearnedDraft
	// SkillID 在修订时标识目标技能包，新建时为空。
	SkillID string
	// ExpectedVersionID 是修订必须匹配的目标版本。
	ExpectedVersionID string
	// ConfirmedPartnerIDs 是用户确认的完整范围，包含来源伙伴。
	ConfirmedPartnerIDs []string
	// AcquirePublication 在文件准备完毕后核对来源授权，并返回事务完成时释放的保护范围。
	AcquirePublication func() (release func(), err error)
	// directMarkdown 记录聊天里明确要求发布的完整技能文档。
	directMarkdown string
	// directDescription 是创建命令单独声明、需与文档一致的技能描述。
	directDescription string
}

// New 创建使用现有技能库和事件发布能力的技能学习服务。
func New(config Config) *Service {
	return &Service{
		dataDirectory: config.DataDirectory,
		skills:        config.Skills,
		wakers:        config.Wakers,
		plugins:       config.Plugins,
		transactions:  config.Transactions,
		events:        config.Events,
		commands:      config.Commands,
	}
}

// ListCandidates 返回有效独立技能和插件派生技能，供学习判定选择或判重。
func (s *Service) ListCandidates(ctx context.Context, wakerID string) ([]Candidate, error) {
	var candidates []Candidate
	err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		candidates = []Candidate{{Name: constants.AgentCLISkillName, Description: "NeuxBot built-in tools"}}
		items, err := s.skills.ListEnabled(ctx, wakerID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.CurrentVersion.ID != "" {
				candidates = append(candidates, candidate(item.SkillPackage, item.CurrentVersion))
			}
		}
		declarations, err := s.plugins.ExecutionDeclarations(ctx, wakerID)
		if err != nil {
			return err
		}
		for _, plugin := range declarations {
			for _, skill := range plugin.Package.Skills {
				candidates = append(candidates, Candidate{
					SkillID: plugin.ID, VersionID: plugin.VersionID, Name: skill.Name, Description: skill.Description,
				})
			}
		}
		return nil
	})
	return candidates, err
}

func candidate(skill models.SkillPackage, version models.SkillVersion) Candidate {
	return Candidate{
		SkillID: skill.ID, VersionID: version.ID, Name: version.Name, Description: version.Description,
		Revisable: !skill.BuiltIn && skill.Source.RevisableByLearning(),
	}
}

// ReadTarget 核对当前版本和实际安装范围，再读取学习修订所需的 SKILL.md。
func (s *Service) ReadTarget(ctx context.Context, wakerID, skillID, expectedVersionID string) (Target, error) {
	var target Target
	var version models.SkillVersion
	err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		var skill models.SkillPackage
		var err error
		skill, version, target.AffectedPartners, err = s.revisionState(ctx, wakerID, skillID, expectedVersionID)
		if err == nil {
			target.Candidate = candidate(skill, version)
		}
		return err
	})
	if err != nil {
		return Target{}, err
	}
	directory, err := s.versionDirectory(version.ContentPath)
	if err != nil {
		return Target{}, err
	}
	markdown, err := skills.ReadVersionMarkdown(directory)
	if err != nil {
		return Target{}, err
	}
	target.Markdown = string(markdown)
	return target, nil
}

// GetReceipt 返回已提交的发布结果，供重启恢复先于重复生成。
func (s *Service) GetReceipt(ctx context.Context, wakerID, suggestionID string) (models.SkillLearningReceipt, bool, error) {
	return s.skills.GetLearningReceipt(ctx, wakerID, suggestionID)
}

// Publish 将不可变版本与安装关系及建议回执原子登记到技能库。
func (s *Service) Publish(ctx context.Context, input Publication) (models.SkillLearningReceipt, error) {
	if input.WakerID == "" || input.SuggestionID == "" {
		return models.SkillLearningReceipt{}, status_error.BadRequest
	}
	if existing, found, err := s.GetReceipt(ctx, input.WakerID, input.SuggestionID); err != nil || found {
		return existing, err
	}
	if (input.SkillID == "") != (input.ExpectedVersionID == "") {
		return models.SkillLearningReceipt{}, status_error.BadRequest
	}

	input.Name = util.NormalizeName(input.Name)
	version, err := s.prepareVersion(ctx, input)
	if err != nil {
		// 同一建议可能在准备期间已由另一尝试完成，优先返回已提交的结果。
		if existing, found, readErr := s.GetReceipt(ctx, input.WakerID, input.SuggestionID); readErr == nil && found {
			return existing, nil
		}
		return models.SkillLearningReceipt{}, err
	}
	receipt := models.SkillLearningReceipt{
		SuggestionID: input.SuggestionID, WakerID: input.WakerID, SkillID: version.SkillPackageID,
		VersionID: version.ID, SkillName: version.Name, CreatedAt: time.Now().UnixMilli(),
	}
	result, err := s.commitPublication(ctx, input, version, receipt)
	if err != nil || result.VersionID != version.ID {
		if removeErr := skills.RemoveVersion(s.dataDirectory, version.ContentPath); removeErr != nil {
			logx.Errorf("remove unpublished learned skill: skill_id=%s version_id=%s error=%v", version.SkillPackageID, version.ID, removeErr)
		}
	}
	return result, err
}

func (s *Service) commitPublication(ctx context.Context, input Publication, version models.SkillVersion, receipt models.SkillLearningReceipt) (models.SkillLearningReceipt, error) {
	if input.AcquirePublication != nil {
		release, err := input.AcquirePublication()
		if err != nil {
			return models.SkillLearningReceipt{}, err
		}
		if release != nil {
			defer release()
		}
	}
	result := models.SkillLearningReceipt{}
	err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		existing, found, err := s.GetReceipt(ctx, input.WakerID, input.SuggestionID)
		if err != nil {
			return err
		}
		if found {
			result = existing
			return nil
		}
		if err := s.writePublication(ctx, input, version); err != nil {
			return err
		}
		if err := s.skills.CreateLearningReceipt(ctx, receipt); err != nil {
			return err
		}
		s.emitResourceChange(ctx, version.SkillPackageID)
		result = receipt
		return nil
	})
	if err != nil {
		return models.SkillLearningReceipt{}, err
	}
	return result, nil
}

func (s *Service) writePublication(ctx context.Context, input Publication, version models.SkillVersion) error {
	if _, err := s.wakers.Get(ctx, input.WakerID); err != nil {
		return err
	}
	wakerIDs := []string{input.WakerID}
	if input.SkillID != "" {
		_, current, partners, err := s.revisionState(ctx, input.WakerID, input.SkillID, input.ExpectedVersionID)
		if err != nil {
			return err
		}
		if current.Name != version.Name {
			return status_error.SkillContentInvalid
		}
		wakerIDs, err = confirmedScope(partners, input.ConfirmedPartnerIDs)
		if err != nil {
			return err
		}
	}
	if version.Name == constants.AgentCLISkillName {
		return status_error.SkillNameConflict
	}
	exists, err := s.skills.EnabledSkillNameExists(ctx, wakerIDs, input.SkillID, version.Name)
	if err != nil {
		return err
	}
	if exists {
		return status_error.SkillNameConflict
	}
	exists, err = s.plugins.EnabledSkillNameExists(ctx, wakerIDs, version.Name)
	if err != nil {
		return err
	}
	if exists {
		return status_error.SkillNameConflict
	}

	if input.SkillID == "" {
		if err := s.skills.CreatePackage(ctx, models.SkillPackage{
			ID: version.SkillPackageID, Name: version.Name, Description: version.Description, Source: constants.SkillSourceLearned,
		}); err != nil {
			return err
		}
	}
	if err := s.skills.CreateVersion(ctx, version, true, input.ExpectedVersionID); err != nil {
		return err
	}
	if input.SkillID == "" {
		if err := s.skills.EnsureWakerSkill(ctx, models.WakerSkill{
			ID: util.New(), WakerID: input.WakerID, SkillPackageID: version.SkillPackageID, Enabled: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) emitResourceChange(ctx context.Context, skillID string) {
	if s.events != nil {
		resource := events.Resource{Kind: "skill", ID: skillID}
		events.AfterCommit(ctx, func() { _ = events.Emit(s.events, "resources", "resources.changed", resource, resource) })
	}
}

func (s *Service) revisionState(ctx context.Context, wakerID, skillID, expectedVersionID string) (models.SkillPackage, models.SkillVersion, []controllers.SkillInstalledPartner, error) {
	skill, err := s.skills.GetPackage(ctx, skillID)
	if err != nil {
		return models.SkillPackage{}, models.SkillVersion{}, nil, err
	}
	partners, err := s.skills.InstalledPartners(ctx, skillID)
	if err != nil {
		return models.SkillPackage{}, models.SkillVersion{}, nil, err
	}
	if !slices.ContainsFunc(partners, func(partner controllers.SkillInstalledPartner) bool { return partner.ID == wakerID }) {
		return models.SkillPackage{}, models.SkillVersion{}, nil, status_error.SkillNotFound
	}
	if skill.BuiltIn || !skill.Source.RevisableByLearning() {
		return models.SkillPackage{}, models.SkillVersion{}, nil, status_error.BadRequest
	}
	if expectedVersionID != "" && skill.CurrentVersionID != expectedVersionID {
		return models.SkillPackage{}, models.SkillVersion{}, nil, status_error.Conflict
	}
	version, err := s.skills.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		return models.SkillPackage{}, models.SkillVersion{}, nil, err
	}
	if version.SkillPackageID != skill.ID {
		return models.SkillPackage{}, models.SkillVersion{}, nil, status_error.SkillVersionNotFound
	}
	return skill, version, partners, nil
}

func confirmedScope(partners []controllers.SkillInstalledPartner, confirmedIDs []string) ([]string, error) {
	ids := make([]string, 0, len(partners))
	for _, partner := range partners {
		ids = append(ids, partner.ID)
	}
	if len(confirmedIDs) == 0 && len(ids) == 1 {
		return ids, nil
	}
	slices.Sort(ids)
	approved := slices.Clone(confirmedIDs)
	slices.Sort(approved)
	if !slices.Equal(approved, ids) {
		return nil, status_error.Conflict
	}
	return ids, nil
}

func (s *Service) prepareVersion(ctx context.Context, input Publication) (models.SkillVersion, error) {
	version := models.SkillVersion{ID: util.New(), SkillPackageID: input.SkillID, Version: "1.0.0", Operation: "learn"}
	var sourceDirectory string
	var sourceDigest string
	var original []byte
	if input.SkillID == "" {
		version.SkillPackageID = util.New()
	} else {
		var current models.SkillVersion
		err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
			_, item, partners, err := s.revisionState(ctx, input.WakerID, input.SkillID, input.ExpectedVersionID)
			if err != nil {
				return err
			}
			current = item
			_, err = confirmedScope(partners, input.ConfirmedPartnerIDs)
			return err
		})
		if err != nil {
			return models.SkillVersion{}, err
		}
		next, ok := skills.NextPatchVersion(current.Version)
		if !ok || current.Name != input.Name {
			return models.SkillVersion{}, status_error.SkillContentInvalid
		}
		version.Version = next
		sourceDirectory, err = s.versionDirectory(current.ContentPath)
		if err != nil {
			return models.SkillVersion{}, err
		}
		sourceDigest = current.ContentDigest
		if input.directMarkdown == "" {
			original, err = skills.ReadVersionMarkdown(sourceDirectory)
			if err != nil {
				return models.SkillVersion{}, err
			}
		}
	}

	var markdown []byte
	var metadata skills.Metadata
	var err error
	if input.directMarkdown == "" {
		markdown, metadata, err = skills.PrepareLearnedMarkdown(original, input.Draft, input.Name)
		if err != nil {
			return models.SkillVersion{}, err
		}
	}
	parent := filepath.Join(s.dataDirectory, "skills")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return models.SkillVersion{}, storageError("prepare learned skill parent", err)
	}
	stage, err := os.MkdirTemp(parent, "version-staging-")
	if err != nil {
		return models.SkillVersion{}, storageError("prepare learned skill stage", err)
	}
	defer func() {
		if err := os.RemoveAll(stage); err != nil {
			logx.Errorf("remove learned skill stage: version_id=%s error=%v", version.ID, err)
		}
	}()
	path := filepath.Join(stage, "SKILL.md")
	if sourceDirectory != "" {
		if err := skills.CopyDirectory(sourceDirectory, stage); err != nil {
			return models.SkillVersion{}, err
		}
	}
	if input.directMarkdown != "" {
		version.Operation = "edit"
		if sourceDirectory != "" {
			digest, err := skills.Digest(stage)
			if err != nil {
				return models.SkillVersion{}, err
			}
			if digest != sourceDigest {
				return models.SkillVersion{}, status_error.SkillContentInvalid
			}
			original, err = skills.ReadVersionMarkdown(stage)
			if err != nil {
				return models.SkillVersion{}, err
			}
		}
		markdown, metadata, err = skills.PrepareManagedMarkdown(original, input.directMarkdown, input.Name, input.directDescription)
		if err != nil {
			return models.SkillVersion{}, err
		}
	}
	if sourceDirectory != "" {
		if err := os.Remove(path); err != nil {
			return models.SkillVersion{}, storageError("replace learned skill document", err)
		}
	}
	if err := os.WriteFile(path, markdown, 0o600); err != nil {
		return models.SkillVersion{}, storageError("write learned skill document", err)
	}
	path, digest, err := skills.CopyVersion(s.dataDirectory, version.SkillPackageID, version.ID, stage)
	if err != nil {
		return models.SkillVersion{}, err
	}
	version.Name, version.Description = metadata.Name, metadata.Description
	version.ContentPath, version.ContentDigest = path, digest
	return version, nil
}

func (s *Service) versionDirectory(contentPath string) (string, error) {
	relative := filepath.Clean(contentPath)
	if !filepath.IsLocal(relative) {
		return "", status_error.SkillContentInvalid
	}
	path := filepath.Join(s.dataDirectory, relative)
	versionsRoot := filepath.Join(s.dataDirectory, "skills", "versions")
	inside, err := filepath.Rel(versionsRoot, path)
	if err != nil || inside == "." || !filepath.IsLocal(inside) || len(strings.Split(inside, string(filepath.Separator))) != 2 {
		return "", status_error.SkillContentInvalid
	}
	return path, nil
}

func storageError(operation string, err error) error {
	logx.Errorf("%s: error=%v", operation, err)
	return status_error.SkillStorageError
}
