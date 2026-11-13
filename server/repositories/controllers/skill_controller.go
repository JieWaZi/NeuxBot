package controllers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SkillController 定义技能包、版本和伙伴关系的持久化操作。
type SkillController interface {
	// ListLibrary 分页查询全局独立技能。
	ListLibrary(context.Context, string, int, int) ([]models.SkillWithState, int64, error)
	// ExistingLibraryContent 按完整内容摘要返回当前技能库资源，不以展示名称判断重复。
	ExistingLibraryContent(context.Context, []string) (map[string]string, error)
	// InstalledCounts 返回实际安装关系总数。
	InstalledCounts(context.Context, []string) (map[string]int64, error)
	// InstalledPartnerPreviews 批量返回每项技能的实际安装伙伴资料预览，包含停用关系。
	InstalledPartnerPreviews(context.Context, []string) (map[string][]models.Waker, error)
	// InstalledPartners 返回单项技能的全部实际安装伙伴及各自启停状态。
	InstalledPartners(context.Context, string) ([]SkillInstalledPartner, error)
	// CreatePackage 创建技能包元数据。
	CreatePackage(ctx context.Context, skill models.SkillPackage) error
	// GetPackage 按标识查询技能包。
	GetPackage(ctx context.Context, id string) (models.SkillPackage, error)
	// ListInstalled 返回伙伴实际安装的技能包及当前版本。
	ListInstalled(ctx context.Context, wakerID string, query string) ([]models.SkillWithState, error)
	// CreateVersion 创建不可变版本，并按预期当前版本条件推进当前指针。
	CreateVersion(ctx context.Context, version models.SkillVersion, makeCurrent bool, expectedCurrentVersionID string) error
	// ListVersions 返回技能的全部版本，按创建时间倒序。
	ListVersions(ctx context.Context, skillID string) ([]models.SkillVersion, error)
	// GetVersion 按标识查询技能版本。
	GetVersion(ctx context.Context, id string) (models.SkillVersion, error)
	// ListVersionContentPaths 返回全部权威技能版本的受管相对路径。
	ListVersionContentPaths(ctx context.Context) ([]string, error)
	// SetCurrentVersion 条件更新技能当前版本指针。
	SetCurrentVersion(ctx context.Context, skillID string, versionID string) error
	// UpsertWakerSkill 创建或更新伙伴的技能启停关系。
	UpsertWakerSkill(ctx context.Context, relation models.WakerSkill) error
	// EnsureWakerSkill 只在关系不存在时创建默认启用状态，不覆盖用户选择。
	EnsureWakerSkill(ctx context.Context, relation models.WakerSkill) error
	// SetWakerSkillEnabled 只更新实际存在的安装关系。
	SetWakerSkillEnabled(ctx context.Context, wakerID string, skillID string, enabled bool) error
	// ListEnabled 返回伙伴当前安装且启用的技能版本。
	ListEnabled(ctx context.Context, wakerID string) ([]models.SkillWithState, error)
	// DeleteWakerSkill 删除伙伴技能关系，使其恢复未关联状态。
	DeleteWakerSkill(ctx context.Context, wakerID string, skillID string) error
	// DeleteByWaker 只删除伙伴安装关系，保留技能库和全部版本。
	DeleteByWaker(ctx context.Context, wakerID string) error
	// DeletePackage 删除技能包、全部版本和全部伙伴关系。
	DeletePackage(ctx context.Context, skillID string) error
	// UpdatePackageMetadata 按已提供字段更新展示名称与分类，不修改版本内容。
	UpdatePackageMetadata(ctx context.Context, skillID string, name string, categories *[]string) error
}

// skillController 使用 GORM 实现技能持久化。
type skillController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewSkillController 创建技能 Repository Controller。
func NewSkillController(db *gorm.DB) SkillController { return &skillController{db: db} }

// CreatePackage 创建技能包，展示名不参与资源身份判断。
func (c *skillController) CreatePackage(ctx context.Context, skill models.SkillPackage) error {
	db := controllerDB(ctx, c.db)
	if err := db.Create(&skill).Error; err != nil {
		if dbhelper.IsDuplicateError(err) {
			return status_error.SkillNameConflict
		}
		return databaseError("create_skill", "skill_id="+skill.ID, err)
	}
	return nil
}

// GetPackage 按标识查询技能包。
func (c *skillController) GetPackage(ctx context.Context, id string) (models.SkillPackage, error) {
	var skill models.SkillPackage
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&skill).Error
	if err == nil {
		return skill, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return skill, status_error.SkillNotFound.WithParams(map[string]interface{}{"SkillID": id})
	}
	return skill, databaseError("get_skill", "skill_id="+id, err)
}

// ListInstalled 查询实际安装关系对应的技能与启停状态。
func (c *skillController) ListInstalled(ctx context.Context, wakerID string, query string) ([]models.SkillWithState, error) {
	var packages []models.SkillPackage
	db := controllerDB(ctx, c.db).Where(
		"id IN (SELECT skill_package_id FROM waker_skills WHERE waker_id = ?)", wakerID,
	)
	if normalized := strings.TrimSpace(query); normalized != "" {
		db = db.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ?", "%"+strings.ToLower(normalized)+"%", "%"+strings.ToLower(normalized)+"%")
	}
	if err := db.Order("built_in DESC, updated_at DESC, id ASC").Find(&packages).Error; err != nil {
		return nil, databaseError("list_installed_skills", "waker_id="+wakerID, err)
	}
	return c.withStates(ctx, wakerID, packages)
}

// ListEnabled 返回伙伴当前实际安装且启用的技能。
func (c *skillController) ListEnabled(ctx context.Context, wakerID string) ([]models.SkillWithState, error) {
	items, err := c.ListInstalled(ctx, wakerID, "")
	if err != nil {
		return nil, err
	}
	result := make([]models.SkillWithState, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *skillController) withStates(ctx context.Context, wakerID string, packages []models.SkillPackage) ([]models.SkillWithState, error) {
	var relations []models.WakerSkill
	if err := controllerDB(ctx, c.db).Where("waker_id = ?", wakerID).Find(&relations).Error; err != nil {
		return nil, databaseError("list_waker_skills", "waker_id="+wakerID, err)
	}
	state := make(map[string]bool, len(relations))
	for _, relation := range relations {
		state[relation.SkillPackageID] = relation.Enabled
	}
	versionIDs := make([]string, 0, len(packages))
	for _, skill := range packages {
		if skill.CurrentVersionID != "" {
			versionIDs = append(versionIDs, skill.CurrentVersionID)
		}
	}
	versions := map[string]models.SkillVersion{}
	for start := 0; start < len(versionIDs); start += 200 {
		end := start + 200
		if end > len(versionIDs) {
			end = len(versionIDs)
		}
		var batch []models.SkillVersion
		if err := controllerDB(ctx, c.db).Where("id IN ?", versionIDs[start:end]).Find(&batch).Error; err != nil {
			return nil, databaseError("list_skill_current_versions", wakerID, err)
		}
		for _, version := range batch {
			versions[version.ID] = version
		}
	}
	result := make([]models.SkillWithState, 0, len(packages))
	for _, skill := range packages {
		item := models.SkillWithState{SkillPackage: skill, Enabled: state[skill.ID]}
		item.CurrentVersion = versions[skill.CurrentVersionID]
		result = append(result, item)
	}
	return result, nil
}

// CreateVersion 创建版本并可将其设置为当前版本。
func (c *skillController) CreateVersion(
	ctx context.Context,
	version models.SkillVersion,
	makeCurrent bool,
	expectedCurrentVersionID string,
) error {
	if version.Name == constants.AgentCLISkillName {
		return status_error.SkillNameConflict
	}
	db := controllerDB(ctx, c.db)
	return NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(transactionContext context.Context) error {
		transaction := controllerDB(transactionContext, db)
		if err := transaction.Create(&version).Error; err != nil {
			return databaseError("create_skill_version", "version_id="+version.ID, err)
		}
		if !makeCurrent {
			return nil
		}
		updates := map[string]any{
			"current_version_id": version.ID,
			"updated_at":         version.CreatedAt,
		}
		if version.Description != "" {
			updates["description"] = version.Description
		}
		query := transaction.Model(&models.SkillPackage{}).Where("id = ?", version.SkillPackageID)
		if expectedCurrentVersionID != "" {
			query = query.Where("current_version_id = ?", expectedCurrentVersionID)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			if dbhelper.IsDuplicateError(result.Error) {
				return status_error.SkillNameConflict
			}
			return databaseError("set_skill_current_version", "skill_id="+version.SkillPackageID, result.Error)
		}
		if result.RowsAffected == 0 {
			return status_error.Conflict
		}
		return nil
	})
}

// ListVersions 返回技能版本历史。
func (c *skillController) ListVersions(ctx context.Context, skillID string) ([]models.SkillVersion, error) {
	var versions []models.SkillVersion
	err := controllerDB(ctx, c.db).Where("skill_package_id = ?", skillID).Order("created_at DESC, id DESC").Find(&versions).Error
	if err != nil {
		return nil, databaseError("list_skill_versions", "skill_id="+skillID, err)
	}
	return versions, nil
}

// GetVersion 按标识查询技能版本。
func (c *skillController) GetVersion(ctx context.Context, id string) (models.SkillVersion, error) {
	var version models.SkillVersion
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&version).Error
	if err == nil {
		return version, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return version, status_error.SkillVersionNotFound.WithParams(map[string]interface{}{"VersionID": id})
	}
	return version, databaseError("get_skill_version", "version_id="+id, err)
}

// ListVersionContentPaths 返回启动清理需要保留的全部权威版本路径。
func (c *skillController) ListVersionContentPaths(ctx context.Context) ([]string, error) {
	var paths []string
	if err := controllerDB(ctx, c.db).
		Model(&models.SkillVersion{}).
		Order("content_path ASC").
		Pluck("content_path", &paths).Error; err != nil {
		return nil, databaseError("list_skill_version_content_paths", "", err)
	}
	return paths, nil
}

// SetCurrentVersion 更新技能当前版本指针。
func (c *skillController) SetCurrentVersion(ctx context.Context, skillID string, versionID string) error {
	result := controllerDB(ctx, c.db).Model(&models.SkillPackage{}).Where("id = ?", skillID).Updates(map[string]any{"current_version_id": versionID, "updated_at": nowMillis()})
	if result.Error != nil {
		return databaseError("set_skill_current_version", "skill_id="+skillID, result.Error)
	}
	return nil
}

// UpsertWakerSkill 创建或更新伙伴启停关系。
func (c *skillController) UpsertWakerSkill(ctx context.Context, relation models.WakerSkill) error {
	db := controllerDB(ctx, c.db)
	var existing models.WakerSkill
	err := db.Where("waker_id = ? AND skill_package_id = ?", relation.WakerID, relation.SkillPackageID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := db.Create(&relation).Error; err != nil {
			return databaseError("create_waker_skill", relation.WakerID, err)
		}
		return nil
	}
	if err != nil {
		return databaseError("get_waker_skill", relation.WakerID, err)
	}
	if err := db.Model(&existing).Updates(map[string]any{"enabled": relation.Enabled, "updated_at": relation.UpdatedAt}).Error; err != nil {
		return databaseError("update_waker_skill", relation.WakerID, err)
	}
	return nil
}

// EnsureWakerSkill 原子补齐默认关系，并保持现有启停状态。
func (c *skillController) EnsureWakerSkill(ctx context.Context, relation models.WakerSkill) error {
	err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "waker_id"}, {Name: "skill_package_id"}},
		DoNothing: true,
	}).Create(&relation).Error
	if err != nil {
		return databaseError("ensure_waker_skill", "waker_id="+relation.WakerID, err)
	}
	return nil
}

// SetWakerSkillEnabled 原子更新启停状态，避免重新创建已移除的安装关系。
func (c *skillController) SetWakerSkillEnabled(ctx context.Context, wakerID string, skillID string, enabled bool) error {
	db := controllerDB(ctx, c.db)
	packages := db.Model(&models.SkillPackage{}).Select("id").Where("id = ?", skillID)
	result := db.Model(&models.WakerSkill{}).
		Where("waker_id = ? AND skill_package_id IN (?)", wakerID, packages).
		Update("enabled", enabled)
	if result.Error != nil {
		return databaseError("update_waker_skill", wakerID, result.Error)
	}
	if result.RowsAffected == 0 {
		return status_error.SkillNotFound.WithParams(map[string]interface{}{"SkillID": skillID})
	}
	return nil
}

// DeleteWakerSkill 删除伙伴技能关系。
func (c *skillController) DeleteWakerSkill(ctx context.Context, wakerID string, skillID string) error {
	if err := controllerDB(ctx, c.db).Where("waker_id = ? AND skill_package_id = ?", wakerID, skillID).Delete(&models.WakerSkill{}).Error; err != nil {
		return databaseError("delete_waker_skill", wakerID, err)
	}
	return nil
}

// DeleteByWaker 仅删除伙伴技能安装关系，不触碰库资源及版本文件。
func (c *skillController) DeleteByWaker(ctx context.Context, wakerID string) error {
	if err := controllerDB(ctx, c.db).Where("waker_id = ?", wakerID).Delete(&models.WakerSkill{}).Error; err != nil {
		return databaseError("delete_waker_skill_relations", "waker_id="+wakerID, err)
	}
	return nil
}

// DeletePackage 删除一个非内置技能包的全部数据库事实。
func (c *skillController) DeletePackage(ctx context.Context, skillID string) error {
	db := controllerDB(ctx, c.db)
	if err := db.Where("skill_package_id = ?", skillID).Delete(&models.WakerSkill{}).Error; err != nil {
		return databaseError("delete_skill_relations", "skill_id="+skillID, err)
	}
	if err := db.Where("skill_package_id = ?", skillID).Delete(&models.SkillVersion{}).Error; err != nil {
		return databaseError("delete_skill_versions", "skill_id="+skillID, err)
	}
	if err := db.Where("id = ?", skillID).Delete(&models.SkillPackage{}).Error; err != nil {
		return databaseError("delete_skill_package", "skill_id="+skillID, err)
	}
	return nil
}

func nowMillis() int64 { return time.Now().UnixMilli() }

// UpdatePackageMetadata 单次更新已提供的展示资料，不改写版本或原技能声明。
func (c *skillController) UpdatePackageMetadata(ctx context.Context, skillID string, name string, categories *[]string) error {
	db := controllerDB(ctx, c.db)
	updates := map[string]any{"updated_at": nowMillis()}
	if name != "" {
		updates["name"] = name
	}
	if categories != nil {
		if err := db.Model(&models.SkillPackage{}).Where("id = ?", skillID).Select("Categories").Updates(&models.SkillPackage{Categories: *categories}).Error; err != nil {
			return databaseError("update_skill_categories", "skill_id="+skillID, err)
		}
	}
	result := db.Model(&models.SkillPackage{}).Where("id = ?", skillID).Updates(updates)
	if result.Error != nil {
		if dbhelper.IsDuplicateError(result.Error) {
			return status_error.SkillNameConflict
		}
		return databaseError("update_skill_metadata", "skill_id="+skillID, result.Error)
	}
	return nil
}

// ListLibrary 分页查询全局独立技能，不将其隐式安装给伙伴。
func (c *skillController) ListLibrary(ctx context.Context, query string, offset, limit int) ([]models.SkillWithState, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.SkillPackage{}).Where("built_in = ? AND source IN ?", false, []constants.SkillSource{constants.SkillSourceGit, constants.SkillSourceZip})
	if query = strings.TrimSpace(query); query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		db = db.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ?", pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_skill_library", "", err)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var packages []models.SkillPackage
	if err := db.Order("built_in DESC, updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&packages).Error; err != nil {
		return nil, 0, databaseError("list_skill_library", "", err)
	}
	result, err := c.withStates(ctx, "", packages)
	return result, total, err
}

// InstalledCounts 批量读取实际安装关系数量。
func (c *skillController) InstalledCounts(ctx context.Context, ids []string) (map[string]int64, error) {
	return installedResourceCounts(ctx, c.db, &models.WakerSkill{}, "skill_package_id", ids)
}

// InstalledPartnerPreviews 先按技能截取伙伴窗口，再批量补齐公开资料，避免逐卡查询及无界头像读取。
func (c *skillController) InstalledPartnerPreviews(ctx context.Context, ids []string) (map[string][]models.Waker, error) {
	result := map[string][]models.Waker{}
	db := controllerDB(ctx, c.db)
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		ranked := db.Model(&models.WakerSkill{}).
			Select("skill_package_id, waker_id, ROW_NUMBER() OVER (PARTITION BY skill_package_id ORDER BY waker_id) AS partner_rank").
			Where("skill_package_id IN ?", ids[start:end])
		var relations []models.WakerSkill
		if err := db.Table("(?) AS ranked_skills", ranked).
			Select("skill_package_id", "waker_id").
			Where("partner_rank <= ?", constants.SkillPartnerPreviewLimit).
			Order("skill_package_id, waker_id").Find(&relations).Error; err != nil {
			return nil, databaseError("read_skill_installed_partners", "", err)
		}
		partnerIDs := []string{}
		seen := map[string]bool{}
		for _, relation := range relations {
			if !seen[relation.WakerID] {
				seen[relation.WakerID] = true
				partnerIDs = append(partnerIDs, relation.WakerID)
			}
		}
		if len(partnerIDs) == 0 {
			continue
		}
		var partners []models.Waker
		if err := db.Select("id", "name", "avatar").Where("id IN ?", partnerIDs).Find(&partners).Error; err != nil {
			return nil, databaseError("read_skill_partner_profiles", "", err)
		}
		profiles := map[string]models.Waker{}
		for _, partner := range partners {
			profiles[partner.ID] = partner
		}
		for _, relation := range relations {
			if partner, ok := profiles[relation.WakerID]; ok {
				result[relation.SkillPackageID] = append(result[relation.SkillPackageID], partner)
			}
		}
	}
	return result, nil
}

// InstalledPartners 仅为技能详情读取完整安装关系，再分批补齐伙伴公开资料。
func (c *skillController) InstalledPartners(ctx context.Context, skillID string) ([]SkillInstalledPartner, error) {
	db := controllerDB(ctx, c.db)
	var relations []models.WakerSkill
	if err := db.Select("waker_id", "enabled").Where("skill_package_id = ?", skillID).Order("waker_id").Find(&relations).Error; err != nil {
		return nil, databaseError("read_skill_installed_partners", "skill_id="+skillID, err)
	}
	result := make([]SkillInstalledPartner, 0, len(relations))
	for start := 0; start < len(relations); start += 100 {
		end := min(start+100, len(relations))
		ids := make([]string, 0, end-start)
		enabled := make(map[string]bool, end-start)
		for _, relation := range relations[start:end] {
			ids = append(ids, relation.WakerID)
			enabled[relation.WakerID] = relation.Enabled
		}
		var partners []models.Waker
		if err := db.Select("id", "name", "avatar").Where("id IN ?", ids).Order("id").Find(&partners).Error; err != nil {
			return nil, databaseError("read_skill_partner_profiles", "skill_id="+skillID, err)
		}
		for _, partner := range partners {
			result = append(result, SkillInstalledPartner{Waker: partner, Enabled: enabled[partner.ID]})
		}
	}
	return result, nil
}

// SkillInstalledPartner 包含安装伙伴的公开资料及技能启停状态。
type SkillInstalledPartner struct {
	// 伙伴的公开身份资料。
	models.Waker
	// Enabled 表示该伙伴是否启用技能。
	Enabled bool
}

// ExistingLibraryContent 每批按索引查找当前独立技能版本，同内容已有多项时稳定选择一个资源。
func (c *skillController) ExistingLibraryContent(ctx context.Context, digests []string) (map[string]string, error) {
	result := map[string]string{}
	db := controllerDB(ctx, c.db)
	for start := 0; start < len(digests); start += 100 {
		end := min(start+100, len(digests))
		var matches []struct {
			// ContentDigest 是当前版本的完整内容摘要。
			ContentDigest string
			// SkillPackageID 是同摘要下稳定选出的真实资源标识。
			SkillPackageID string
		}
		current := db.Model(&models.SkillPackage{}).Select("current_version_id").Where("built_in = ? AND source IN ?", false, []constants.SkillSource{constants.SkillSourceGit, constants.SkillSourceZip})
		if err := db.Model(&models.SkillVersion{}).Select("content_digest, MIN(skill_package_id) AS skill_package_id").Where("content_digest IN ? AND id IN (?)", digests[start:end], current).Group("content_digest").Find(&matches).Error; err != nil {
			return nil, databaseError("find_existing_library_skill_content", "", err)
		}
		for _, match := range matches {
			result[match.ContentDigest] = match.SkillPackageID
		}
	}
	return result, nil
}
