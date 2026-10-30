package controllers

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PluginController 管理插件的唯一资源、版本、安装与共享配置事实。
type PluginController interface {
	// ContentReferences 返回数据库仍保留的受管目录相对路径。
	ContentReferences(context.Context) ([]string, error)
	// InstalledReferences 只读取伙伴已启用安装的展示身份和固定版本。
	InstalledReferences(context.Context, string) ([]models.PluginReference, error)
	// ExecutionDeclarations 在短读事务内声明伙伴启用安装的固定版本与配置修订。
	ExecutionDeclarations(context.Context, string) ([]models.PluginExecutionDeclaration, error)
	// ExecutionConfiguration 在冷资源取得时解密共享配置，不读取私有授权交付材料。
	ExecutionConfiguration(context.Context, string) (models.PluginConfiguration, error)
	// ReadStates 批量补齐最多一百个资源的版本、安装与配置。
	ReadStates(context.Context, []string) (map[string]models.PluginState, error)
	// Create 登记稳定资源。
	Create(context.Context, models.Plugin) error
	// Get 查询资源，缺失返回明确领域错误。
	Get(context.Context, string) (models.Plugin, error)
	// FindSource 按归属和来源查询资源，未登记时返回空资源。
	FindSource(context.Context, string, string) (models.Plugin, error)
	// List 按归属、安装范围和关键字分页查询资源。
	List(context.Context, string, string, string, int, int, bool) ([]models.Plugin, int64, error)
	// Update 修改资源展示资料。
	Update(context.Context, string, string, string) error
	// PutVersion 幂等登记不可变版本，相同版本不同摘要拒绝。
	PutVersion(context.Context, models.PluginVersion) (models.PluginVersion, error)
	// Version 查询明确版本。
	Version(context.Context, string) (models.PluginVersion, error)
	// Versions 查询资源版本分布。
	Versions(context.Context, string) ([]models.PluginVersion, error)
	// Install 新建安装关系，不隐式覆盖旧版本或启停。
	Install(context.Context, models.WakerPlugin) error
	// Installations 按插件或伙伴查询真实安装。
	Installations(context.Context, string, string) ([]models.WakerPlugin, error)
	// InstalledVersionsExcept 返回移除指定伙伴关系或整个资源后仍被安装引用的版本。
	InstalledVersionsExcept(context.Context, string, string) ([]models.PluginVersion, error)
	// SetEnabled 修改已有安装的启停。
	SetEnabled(context.Context, string, string, bool) error
	// SwitchVersion 按旧版本条件切换已有安装。
	SwitchVersion(context.Context, string, string, string, string) error
	// Uninstall 仅移除一个伙伴的安装关系。
	Uninstall(context.Context, string, string) error
	// Configuration 返回可在服务端使用的共享配置。
	Configuration(context.Context, string) (models.PluginConfiguration, error)
	// UpdateConfiguration 条件更新加密共享配置，由完整用例控制事务。
	UpdateConfiguration(context.Context, models.PluginConfiguration, int64) error
	// UpdateDeliveryMaterials 只保存同一配置修订的加密授权交付材料，不改变配置身份。
	UpdateDeliveryMaterials(context.Context, models.PluginConfiguration) error
	// Delete 删除资源和关联元数据，由调用用例控制事务和活跃内容回收。
	Delete(context.Context, string) error
	// DeleteByWaker 删除伙伴安装和自有资源，由伙伴聚合删除事务调用。
	DeleteByWaker(context.Context, string) error
}

// pluginController 通过当前 Context 的事务连接执行单表持久化。
type pluginController struct {
	// cleanup 在资源删除已提交后尝试回收无引用内容。
	cleanup func()
	// db 是事务外默认连接。
	db *gorm.DB
}

// NewPluginController 创建插件 Repository。
func NewPluginController(db *gorm.DB, cleanup ...func()) PluginController {
	c := &pluginController{db: db}
	if len(cleanup) > 0 {
		c.cleanup = cleanup[0]
	}
	return c
}

// Create 登记资源，来源冲突由用例重新读取既有身份。
func (c *pluginController) Create(ctx context.Context, p models.Plugin) error {
	err := controllerDB(ctx, c.db).Create(&p).Error
	if err != nil && dbhelper.IsDuplicateError(err) {
		return status_error.PluginVersionConflict
	}
	if err != nil {
		return databaseError("create_plugin", p.ID, err)
	}
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", p.ID))
	return nil
}

// Get 查询明确资源。
func (c *pluginController) Get(ctx context.Context, id string) (models.Plugin, error) {
	var p models.Plugin
	result := controllerDB(ctx, c.db).Where("id = ?", id).Limit(1).Find(&p)
	if result.Error != nil {
		return p, databaseError("get_plugin", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return p, status_error.PluginNotFound
	}
	return p, nil
}

// FindSource 查询明确来源，不凭展示名判重。
func (c *pluginController) FindSource(ctx context.Context, owner, key string) (models.Plugin, error) {
	var p models.Plugin
	err := controllerDB(ctx, c.db).Where("owner_waker_id = ? AND source_key = ?", owner, key).Limit(1).Find(&p).Error
	if err != nil {
		return p, databaseError("find_plugin_source", key, err)
	}
	return p, nil
}

// List 将全局发现和伙伴真实安装分开，统计不补齐展示资料。
func (c *pluginController) List(ctx context.Context, owner, waker, query string, offset, limit int, installedOnly bool) ([]models.Plugin, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.Plugin{})
	if waker != "" {
		db = db.Where("owner_waker_id = ? OR id IN (SELECT plugin_id FROM waker_plugins WHERE waker_id = ?)", waker, waker)
	} else {
		db = db.Where("owner_waker_id = ?", owner)
	}
	if installedOnly {
		db = db.Where("id IN (SELECT plugin_id FROM waker_plugins)")
	}
	if query = strings.TrimSpace(query); query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		db = db.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ? OR source_key = ?", pattern, pattern, query)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_plugins", waker, err)
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result := []models.Plugin{}
	if err := db.Order("updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&result).Error; err != nil {
		return nil, 0, databaseError("list_plugins", waker, err)
	}
	return result, total, nil
}

// Update 修改可变展示资料，不修改包内容。
func (c *pluginController) Update(ctx context.Context, id, name, description string) error {
	err := controllerDB(ctx, c.db).Model(&models.Plugin{}).Where("id = ?", id).Updates(map[string]any{"name": name, "description": description, "updated_at": nowMillis()}).Error
	if err != nil {
		return databaseError("update_plugin", id, err)
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("plugin", id))
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", id))
	return nil
}

// PutVersion 幂等登记版本，不能让相同版本号改指其他内容。
func (c *pluginController) PutVersion(ctx context.Context, v models.PluginVersion) (models.PluginVersion, error) {
	db := controllerDB(ctx, c.db)
	if _, err := c.Get(ctx, v.PluginID); err != nil {
		return v, err
	}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "version"}}, DoNothing: true}).Create(&v).Error; err != nil {
		return v, databaseError("create_plugin_version", v.PluginID, err)
	}
	var stored models.PluginVersion
	if err := db.Where("plugin_id = ? AND version = ?", v.PluginID, v.Version).Find(&stored).Error; err != nil {
		return v, databaseError("get_plugin_version", v.PluginID, err)
	}
	if stored.Digest != v.Digest {
		return v, status_error.PluginVersionConflict
	}
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", v.PluginID))
	return stored, nil
}

// Version 查询指定不可变版本。
func (c *pluginController) Version(ctx context.Context, id string) (models.PluginVersion, error) {
	var v models.PluginVersion
	result := controllerDB(ctx, c.db).Where("id = ?", id).Limit(1).Find(&v)
	if result.Error != nil {
		return v, databaseError("get_plugin_version", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return v, status_error.PluginNotFound
	}
	return v, nil
}

// Versions 按确定顺序返回资源保留的不可变版本。
func (c *pluginController) Versions(ctx context.Context, id string) ([]models.PluginVersion, error) {
	result := []models.PluginVersion{}
	err := controllerDB(ctx, c.db).Where("plugin_id = ?", id).Order("created_at DESC, id ASC").Find(&result).Error
	if err != nil {
		return nil, databaseError("list_plugin_versions", id, err)
	}
	return result, nil
}

// InstalledVersionsExcept 用安装子查询限定当前版本，不展开历史或使用 JOIN。
func (c *pluginController) InstalledVersionsExcept(ctx context.Context, plugin, waker string) ([]models.PluginVersion, error) {
	db := controllerDB(ctx, c.db)
	installed := db.Model(&models.WakerPlugin{}).Select("version_id")
	if waker == "" {
		installed = installed.Where("plugin_id <> ?", plugin)
	} else {
		installed = installed.Where("plugin_id <> ? OR waker_id <> ?", plugin, waker)
	}
	versions := []models.PluginVersion{}
	if err := db.Select("id", "plugin_id", "package").Where("id IN (?)", installed).Find(&versions).Error; err != nil {
		return nil, databaseError("remaining_plugin_installation_versions", plugin, err)
	}
	return versions, nil
}

// Install 校验归属和版本，重复安装不重启停用关系。
func (c *pluginController) Install(ctx context.Context, r models.WakerPlugin) error {
	p, err := c.Get(ctx, r.PluginID)
	if err != nil {
		return err
	}
	if p.OwnerWakerID != "" && p.OwnerWakerID != r.WakerID {
		return status_error.PluginForbidden
	}
	v, err := c.Version(ctx, r.VersionID)
	if err != nil {
		return err
	}
	if v.PluginID != r.PluginID {
		return status_error.PluginVersionConflict
	}
	db := controllerDB(ctx, c.db)
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "waker_id"}, {Name: "plugin_id"}}, DoNothing: true}).Create(&r).Error; err != nil {
		return databaseError("install_plugin", r.PluginID, err)
	}
	var stored models.WakerPlugin
	if err := db.Where("waker_id = ? AND plugin_id = ?", r.WakerID, r.PluginID).Find(&stored).Error; err != nil {
		return databaseError("get_plugin_install", r.PluginID, err)
	}
	if stored.VersionID != r.VersionID {
		return status_error.PluginVersionConflict
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("waker", r.WakerID))
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", r.PluginID))
	return nil
}

// Installations 查询真实关系，包括停用关系，供版本兼容和影响分析使用。
func (c *pluginController) Installations(ctx context.Context, plugin, waker string) ([]models.WakerPlugin, error) {
	result := []models.WakerPlugin{}
	db := controllerDB(ctx, c.db)
	if plugin != "" {
		db = db.Where("plugin_id = ?", plugin)
	}
	if waker != "" {
		db = db.Where("waker_id = ?", waker)
	}
	if plugin == "" && waker == "" {
		return result, status_error.BadRequest
	}
	if err := db.Order("waker_id ASC, id ASC").Find(&result).Error; err != nil {
		return nil, databaseError("list_plugin_installs", plugin, err)
	}
	return result, nil
}

// SetEnabled 修改指定关系，不新建未安装占位。
func (c *pluginController) SetEnabled(ctx context.Context, waker, plugin string, enabled bool) error {
	result := controllerDB(ctx, c.db).Model(&models.WakerPlugin{}).Where("waker_id = ? AND plugin_id = ?", waker, plugin).Updates(map[string]any{"enabled": enabled, "updated_at": nowMillis()})
	if result.Error != nil {
		return databaseError("set_plugin_enabled", plugin, result.Error)
	}
	if result.RowsAffected == 0 {
		return status_error.PluginNotFound
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("waker", waker))
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", plugin))
	return nil
}

// SwitchVersion 校验版本属于当前资源，并执行旧版本条件更新。
func (c *pluginController) SwitchVersion(ctx context.Context, waker, plugin, expected, next string) error {
	v, err := c.Version(ctx, next)
	if err != nil {
		return err
	}
	if v.PluginID != plugin {
		return status_error.PluginVersionConflict
	}
	result := controllerDB(ctx, c.db).Model(&models.WakerPlugin{}).Where("waker_id = ? AND plugin_id = ? AND version_id = ?", waker, plugin, expected).Updates(map[string]any{"version_id": next, "updated_at": nowMillis()})
	if result.Error != nil {
		return databaseError("switch_plugin_version", plugin, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginVersionConflict
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("waker", waker))
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", plugin))
	return nil
}

// Uninstall 只移除指定伙伴关系。
func (c *pluginController) Uninstall(ctx context.Context, waker, plugin string) error {
	err := controllerDB(ctx, c.db).Where("waker_id = ? AND plugin_id = ?", waker, plugin).Delete(&models.WakerPlugin{}).Error
	if err != nil {
		return databaseError("uninstall_plugin", plugin, err)
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("waker", waker))
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", plugin))
	return nil
}

// Configuration 返回本机资源唯一配置；缺失表示修订零。
func (c *pluginController) Configuration(ctx context.Context, id string) (models.PluginConfiguration, error) {
	result := models.PluginConfiguration{PluginID: id, Values: map[string]string{}, Secrets: []models.MCPSecretDefinition{}}
	db := controllerDB(ctx, c.db)
	if err := db.Where("plugin_id = ?", id).Limit(1).Find(&result).Error; err != nil {
		return result, databaseError("get_plugin_configuration", id, err)
	}
	if result.Values == nil {
		result.Values = map[string]string{}
	}
	if result.Secrets == nil {
		result.Secrets = []models.MCPSecretDefinition{}
	}
	if len(result.Secrets) > 0 || len(result.DeliveryMaterials) > 0 {
		key, err := getMCPStorageKey(db)
		if err != nil {
			return result, err
		}
		if err := decryptSecretDefinitions(key, id, "plugin", result.Secrets); err != nil {
			return result, err
		}
		if err := decryptSecretDefinitions(key, id, "plugin-delivery", result.DeliveryMaterials); err != nil {
			return result, err
		}
	}
	return result, nil
}

// UpdateConfiguration 加密后按修订写入，由拥有完整用例的事务统一提交。
func (c *pluginController) UpdateConfiguration(ctx context.Context, config models.PluginConfiguration, expected int64) error {
	db := controllerDB(ctx, c.db)
	if _, err := c.Get(ctx, config.PluginID); err != nil {
		return err
	}
	if config.Values == nil {
		config.Values = map[string]string{}
	}
	if config.Secrets == nil {
		config.Secrets = []models.MCPSecretDefinition{}
	}
	config.Secrets = append([]models.MCPSecretDefinition(nil), config.Secrets...)
	if len(config.Secrets) > 0 {
		key, err := ensureMCPStorageKey(db)
		if err != nil {
			return err
		}
		if err := encryptSecretDefinitions(key, config.PluginID, "plugin", config.Secrets); err != nil {
			return err
		}
	}
	config.DeliveryMaterials = append([]models.MCPSecretDefinition(nil), config.DeliveryMaterials...)
	if len(config.DeliveryMaterials) > 0 {
		key, err := ensureMCPStorageKey(db)
		if err != nil {
			return err
		}
		if err := encryptSecretDefinitions(key, config.PluginID, "plugin-delivery", config.DeliveryMaterials); err != nil {
			return err
		}
	}
	config.Revision = expected + 1
	config.UpdatedAt = nowMillis()
	result := db.Model(&models.PluginConfiguration{}).Where("plugin_id = ? AND revision = ?", config.PluginID, expected).
		Select("Revision", "Values", "Secrets", "DeliveryMaterials", "UpdatedAt").Updates(&config)
	if result.Error == nil && result.RowsAffected == 0 && expected == 0 {
		result = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&config)
	}
	if result.Error != nil {
		return databaseError("update_plugin_configuration", config.PluginID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginConfigurationConflict
	}
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", config.PluginID))
	return nil
}

// Delete 删除完整本机元数据，内容回收在数据库提交之后处理。
// UpdateDeliveryMaterials 复用配置密钥与乐观并发事实，凭据刷新不增加配置修订。
func (c *pluginController) UpdateDeliveryMaterials(ctx context.Context, config models.PluginConfiguration) error {
	db := controllerDB(ctx, c.db)
	if _, err := c.Get(ctx, config.PluginID); err != nil {
		return err
	}
	config.DeliveryMaterials = append([]models.MCPSecretDefinition(nil), config.DeliveryMaterials...)
	if len(config.DeliveryMaterials) > 0 {
		key, err := ensureMCPStorageKey(db)
		if err != nil {
			return err
		}
		if err := encryptSecretDefinitions(key, config.PluginID, "plugin-delivery", config.DeliveryMaterials); err != nil {
			return err
		}
	}
	result := db.Model(&models.PluginConfiguration{}).Where("plugin_id = ? AND revision = ?", config.PluginID, config.Revision).
		Select("DeliveryMaterials").Updates(&config)
	if result.Error == nil && result.RowsAffected == 0 && config.Revision == 0 {
		config.Values = map[string]string{}
		result = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&config)
	}
	if result.Error != nil {
		return databaseError("update_plugin_delivery_materials", config.PluginID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginConfigurationConflict
	}
	return nil
}

func (c *pluginController) Delete(ctx context.Context, id string) error {
	db := controllerDB(ctx, c.db)
	for _, model := range []any{&models.WakerPlugin{}, &models.PluginConfiguration{}, &models.PluginVersion{}} {
		if err := db.Where("plugin_id = ?", id).Delete(model).Error; err != nil {
			return databaseError("delete_plugin_relations", id, err)
		}
	}
	if err := db.Where("id = ?", id).Delete(&models.Plugin{}).Error; err != nil {
		return databaseError("delete_plugin", id, err)
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("plugin", id))
	publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", id))
	if c.cleanup != nil {
		events.AfterCommit(ctx, c.cleanup)
	}
	return nil
}

// DeleteByWaker 删除当前伙伴关系及其自有资源，不影响其他全局安装。
func (c *pluginController) DeleteByWaker(ctx context.Context, waker string) error {
	relations, err := c.Installations(ctx, "", waker)
	if err != nil {
		return err
	}
	db := controllerDB(ctx, c.db)
	var owned []models.Plugin
	if err := db.Where("owner_waker_id = ?", waker).Find(&owned).Error; err != nil {
		return databaseError("list_owned_plugins", waker, err)
	}
	for _, p := range owned {
		if err := c.Delete(ctx, p.ID); err != nil {
			return err
		}
	}
	if err := db.Where("waker_id = ?", waker).Delete(&models.WakerPlugin{}).Error; err != nil {
		return databaseError("delete_waker_plugins", waker, err)
	}
	ownedIDs := map[string]bool{}
	for _, p := range owned {
		ownedIDs[p.ID] = true
	}
	for _, relation := range relations {
		if !ownedIDs[relation.PluginID] {
			publishCommittedChange(ctx, controllerDB(ctx, c.db), constants.PluginEventTopic, eventsResource("plugin", relation.PluginID))
		}
	}
	publishReferenceChange(ctx, controllerDB(ctx, c.db), eventsResource("waker", waker))
	return nil
}

// ReadStates 在一个有界资源窗口批量读取关联事实，避免逐资源查询。
func (c *pluginController) ReadStates(ctx context.Context, ids []string) (map[string]models.PluginState, error) {
	result := map[string]models.PluginState{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 100 {
		return nil, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	var versions []models.PluginVersion
	if err := db.Where("plugin_id IN ?", ids).Order("created_at DESC, id ASC").Find(&versions).Error; err != nil {
		return nil, databaseError("read_plugin_versions", "", err)
	}
	for _, id := range ids {
		result[id] = models.PluginState{Versions: []models.PluginVersion{}, Installations: []models.WakerPlugin{}, Configuration: models.PluginConfiguration{PluginID: id, Values: map[string]string{}, Secrets: []models.MCPSecretDefinition{}}}
	}
	var resources []models.Plugin
	if err := db.Where("id IN ?", ids).Find(&resources).Error; err != nil {
		return nil, databaseError("read_plugin_resources", "", err)
	}
	for _, resource := range resources {
		item := result[resource.ID]
		item.Plugin = resource
		item.PartnerNames = map[string]string{}
		item.PartnerAvatars = map[string]string{}
		result[resource.ID] = item
	}
	for _, version := range versions {
		item := result[version.PluginID]
		item.Versions = append(item.Versions, version)
		result[version.PluginID] = item
	}
	var installs []models.WakerPlugin
	if err := db.Where("plugin_id IN ?", ids).Order("waker_id ASC, id ASC").Find(&installs).Error; err != nil {
		return nil, databaseError("read_plugin_installs", "", err)
	}
	partnerIDs := []string{}
	seenPartners := map[string]bool{}
	for _, install := range installs {
		if !seenPartners[install.WakerID] {
			seenPartners[install.WakerID] = true
			partnerIDs = append(partnerIDs, install.WakerID)
		}
	}
	names := map[string]string{}
	avatars := map[string]string{}
	for start := 0; start < len(partnerIDs); start += 100 {
		end := min(start+100, len(partnerIDs))
		var partners []models.Waker
		if err := db.Select("id", "name", "avatar").Where("id IN ?", partnerIDs[start:end]).Find(&partners).Error; err != nil {
			return nil, databaseError("read_plugin_partners", "", err)
		}
		for _, partner := range partners {
			names[partner.ID] = partner.Name
			avatars[partner.ID] = partner.Avatar
		}
	}
	for _, install := range installs {
		item := result[install.PluginID]
		item.Installations = append(item.Installations, install)
		if item.PartnerNames == nil {
			item.PartnerNames = map[string]string{}
		}
		if item.PartnerAvatars == nil {
			item.PartnerAvatars = map[string]string{}
		}
		item.PartnerNames[install.WakerID] = names[install.WakerID]
		item.PartnerAvatars[install.WakerID] = avatars[install.WakerID]
		result[install.PluginID] = item
	}
	var configs []models.PluginConfiguration
	if err := db.Where("plugin_id IN ?", ids).Find(&configs).Error; err != nil {
		return nil, databaseError("read_plugin_configurations", "", err)
	}
	var key []byte
	for _, config := range configs {
		if len(config.Secrets) > 0 || len(config.DeliveryMaterials) > 0 {
			if key == nil {
				var err error
				key, err = getMCPStorageKey(db)
				if err != nil {
					return nil, err
				}
			}
			if err := decryptSecretDefinitions(key, config.PluginID, "plugin", config.Secrets); err != nil {
				return nil, err
			}
			if err := decryptSecretDefinitions(key, config.PluginID, "plugin-delivery", config.DeliveryMaterials); err != nil {
				return nil, err
			}
		}
		if config.Values == nil {
			config.Values = map[string]string{}
		}
		if config.Secrets == nil {
			config.Secrets = []models.MCPSecretDefinition{}
		}
		item := result[config.PluginID]
		item.Configuration = config
		result[config.PluginID] = item
	}
	return result, nil
}

// ExecutionDeclarations 只投影当前伙伴启用的固定事实，历史版本和管理展示不参与执行声明。
func (c *pluginController) ExecutionDeclarations(ctx context.Context, waker string) ([]models.PluginExecutionDeclaration, error) {
	result := []models.PluginExecutionDeclaration{}
	err := NewTransactionController(dbhelper.NewGormTransactionManager(c.db)).Transaction(ctx, func(readContext context.Context) error {
		tx := controllerDB(readContext, c.db)
		// 事务重试必须丢弃前次快照，只返回本次一致读取的声明。
		result = result[:0]
		var installations []models.WakerPlugin
		if err := tx.Select("plugin_id", "version_id").Where("waker_id = ? AND enabled = ?", waker, true).Order("plugin_id").Find(&installations).Error; err != nil {
			return databaseError("execution_plugin_installations", waker, err)
		}
		if len(installations) == 0 {
			return nil
		}
		installed := func(column string) *gorm.DB {
			return tx.Model(&models.WakerPlugin{}).Select(column).Where("waker_id = ? AND enabled = ?", waker, true)
		}
		var resources []models.Plugin
		if err := tx.Select("id", "name").Where("id IN (?)", installed("plugin_id")).Where("owner_waker_id = ? OR owner_waker_id = ?", "", waker).Find(&resources).Error; err != nil {
			return databaseError("execution_plugin_resources", waker, err)
		}
		var versions []models.PluginVersion
		if err := tx.Select("id", "plugin_id", "digest", "trusted_at").Where("id IN (?)", installed("version_id")).Find(&versions).Error; err != nil {
			return databaseError("execution_plugin_versions", waker, err)
		}
		var configurations []models.PluginConfiguration
		if err := tx.Select("plugin_id", "revision").Where("plugin_id IN (?)", installed("plugin_id")).Find(&configurations).Error; err != nil {
			return databaseError("execution_plugin_revisions", waker, err)
		}
		byID := make(map[string]models.PluginVersion, len(versions))
		for _, version := range versions {
			byID[version.ID] = version
		}
		names := make(map[string]string, len(resources))
		for _, resource := range resources {
			names[resource.ID] = resource.Name
		}
		revisions := make(map[string]int64, len(configurations))
		for _, configuration := range configurations {
			revisions[configuration.PluginID] = configuration.Revision
		}
		for _, installation := range installations {
			version, exists := byID[installation.VersionID]
			name, owned := names[installation.PluginID]
			if !exists || !owned || version.PluginID != installation.PluginID {
				continue
			}
			result = append(result, models.PluginExecutionDeclaration{ID: installation.PluginID, Name: name, VersionID: version.ID, Digest: version.Digest, TrustedAt: version.TrustedAt, ConfigurationRevision: revisions[installation.PluginID]})
		}
		return nil
	})
	return result, err
}

// ExecutionConfiguration 延迟解密实际执行配置，授权私有材料仍由身份租约持有者单独读取。
func (c *pluginController) ExecutionConfiguration(ctx context.Context, id string) (models.PluginConfiguration, error) {
	result := models.PluginConfiguration{PluginID: id, Values: map[string]string{}, Secrets: []models.MCPSecretDefinition{}}
	db := controllerDB(ctx, c.db)
	if err := db.Select("plugin_id", "revision", "values", "secrets").Where("plugin_id = ?", id).Limit(1).Find(&result).Error; err != nil {
		return result, databaseError("execution_plugin_configuration", id, err)
	}
	if len(result.Secrets) > 0 {
		key, err := getMCPStorageKey(db)
		if err != nil {
			return result, err
		}
		if err := decryptSecretDefinitions(key, id, "plugin", result.Secrets); err != nil {
			return result, err
		}
	}
	return result, nil
}

// ContentReferences 以两次单表查询覆盖共享版本和插件实例数据。
func (c *pluginController) ContentReferences(ctx context.Context) ([]string, error) {
	db := controllerDB(ctx, c.db)
	var paths []string
	if err := db.Model(&models.PluginVersion{}).Pluck("content_path", &paths).Error; err != nil {
		return nil, databaseError("plugin_content_references", "", err)
	}
	var ids []string
	if err := db.Model(&models.Plugin{}).Pluck("id", &ids).Error; err != nil {
		return nil, databaseError("plugin_data_references", "", err)
	}
	for _, id := range ids {
		paths = append(paths, "plugins/data/"+id)
	}
	return paths, nil
}

// InstalledReferences 以三次单表窄列查询读取输入引用，不读取包、配置、账号或使用状态。
// 调用方以短读事务固定三个查询的一致性；IN 子查询复用伙伴安装的索引。
func (c *pluginController) InstalledReferences(ctx context.Context, waker string) ([]models.PluginReference, error) {
	db := controllerDB(ctx, c.db)
	var installations []models.WakerPlugin
	if err := db.Select("plugin_id", "version_id").Where("waker_id = ? AND enabled = ?", waker, true).Order("plugin_id").Find(&installations).Error; err != nil {
		return nil, databaseError("plugin_reference_installations", waker, err)
	}
	result := make([]models.PluginReference, 0, len(installations))
	if len(installations) == 0 {
		return result, nil
	}
	installed := func(column string) *gorm.DB {
		return db.Model(&models.WakerPlugin{}).Select(column).Where("waker_id = ? AND enabled = ?", waker, true)
	}
	var resources []models.Plugin
	if err := db.Select("id", "name", "description").Where("id IN (?)", installed("plugin_id")).Where("owner_waker_id = ? OR owner_waker_id = ?", "", waker).Order("name, id").Find(&resources).Error; err != nil {
		return nil, databaseError("plugin_reference_resources", waker, err)
	}
	var versions []models.PluginVersion
	if err := db.Select("id", "plugin_id", "version").Where("id IN (?)", installed("version_id")).Find(&versions).Error; err != nil {
		return nil, databaseError("plugin_reference_versions", waker, err)
	}
	pinned := make(map[string]string, len(installations))
	for _, relation := range installations {
		pinned[relation.PluginID] = relation.VersionID
	}
	byID := make(map[string]models.PluginVersion, len(versions))
	for _, version := range versions {
		byID[version.ID] = version
	}
	for _, resource := range resources {
		version, exists := byID[pinned[resource.ID]]
		if exists && version.PluginID == resource.ID {
			result = append(result, models.PluginReference{ID: resource.ID, Name: resource.Name, Description: resource.Description, Version: version.Version, VersionID: version.ID})
		}
	}
	return result, nil
}

// publishReferenceChange 仅广播候选身份实际改变的资源，配置与使用状态仍走管理事件。
func publishReferenceChange(ctx context.Context, db *gorm.DB, resource events.Resource) {
	value, exists := db.Get(eventPublisherSetting)
	publisher, ok := value.(events.Publisher)
	if !exists || !ok {
		return
	}
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, constants.PluginEventTopic, constants.PluginReferencesChangedEventType, resource, resource)
	})
}
