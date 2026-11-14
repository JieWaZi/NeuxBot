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
	// BrowseInstalled 过滤、计数并分页读取全局实际安装的插件资源。
	BrowseInstalled(context.Context, string, int, int) ([]models.Plugin, int64, error)
	// BrowseLocalUninstalled 分页读取尚无伙伴安装的本地技能插件，零窗口仅统计总量。
	BrowseLocalUninstalled(context.Context, string, int, int) ([]models.Plugin, int64, error)
	// InstalledSources 返回给定有界来源集合中已存在真实安装的来源。
	InstalledSources(context.Context, []string) ([]string, error)
	// ReadBrowseStates 只补齐有界窗口的实际安装版本或未安装本地初始版本公开摘要与真实安装数量。
	ReadBrowseStates(context.Context, []string) (map[string]models.PluginBrowseState, error)
	// ContentReferences 返回数据库仍保留的受管目录相对路径。
	ContentReferences(context.Context) ([]string, error)
	// RetireUnusedVersions 将已失去引用和准备窗口的目录包收敛为版本与摘要记录。
	RetireUnusedVersions(context.Context, []string) error
	// InstalledReferences 只读取伙伴已启用安装的展示身份和固定版本。
	InstalledReferences(context.Context, string) ([]models.PluginReference, error)
	// ExecutionDeclarations 在短读事务内声明伙伴启用安装的固定版本与配置修订。
	ExecutionDeclarations(context.Context, string) ([]models.PluginExecutionDeclaration, error)
	// ExecutionConfiguration 在冷资源取得时解密共享配置，不读取私有授权交付材料。
	ExecutionConfiguration(context.Context, string) (models.PluginConfiguration, error)
	// ReadStates 批量补齐最多一百个资源的版本、安装及公开配置修订。
	ReadStates(context.Context, []string) (map[string]models.PluginState, error)
	// ReadInstalledStates 为伙伴列表仅补齐实际使用的版本与安装事实。
	ReadInstalledStates(context.Context, []string) (map[string]models.PluginState, error)
	// Create 登记稳定资源。
	Create(context.Context, models.Plugin) error
	// Get 查询资源，缺失返回明确领域错误。
	Get(context.Context, string) (models.Plugin, error)
	// FindSource 按来源查询资源，未登记时返回空资源。
	FindSource(context.Context, string) (models.Plugin, error)
	// List 按安装范围和关键字分页查询资源。
	List(context.Context, string, string, int, int) ([]models.Plugin, int64, error)
	// Update 修改资源展示资料。
	Update(context.Context, string, string, string, *[]string) error
	// PutVersion 幂等登记不可变版本，相同版本不同摘要拒绝。
	PutVersion(context.Context, models.PluginVersion) (models.PluginVersion, error)
	// Version 查询明确版本。
	Version(context.Context, string) (models.PluginVersion, error)
	// Versions 查询资源版本分布。
	Versions(context.Context, string) ([]models.PluginVersion, error)
	// LatestVersion 返回最近登记的可用本地版本。
	LatestVersion(context.Context, string) (models.PluginVersion, error)
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
	// SetRollbackVersion 依附安装切换事务记录上一成功安装版本，空值清除入口。
	SetRollbackVersion(context.Context, string, string) error
	// Uninstall 由卸载事务调用，移除一个伙伴的安装关系。
	Uninstall(context.Context, string, string) error
	// Configuration 返回可在服务端使用的共享配置。
	Configuration(context.Context, string) (models.PluginConfiguration, error)
	// UpdateConfiguration 条件更新加密共享配置，由完整用例控制事务。
	UpdateConfiguration(context.Context, models.PluginConfiguration, int64) error
	// ConfigurationRevision 只读取普通配置修订，不解密配置。
	ConfigurationRevision(context.Context, string) (int64, error)
	// AccountGeneration 只读取共享账号代次，不读取或解密材料。
	AccountGeneration(context.Context, string) (int64, error)
	// Account 仅读取实际管理或执行所需的共享私有材料。
	Account(context.Context, string) (models.PluginAccount, error)
	// UpdateAccount 按预期代次原子替换加密材料并推进账号代次。
	UpdateAccount(context.Context, models.PluginAccount, int64) error
	// Delete 删除资源和关联元数据，由调用用例控制事务和活跃内容回收。
	Delete(context.Context, string) error
	// DeleteByWaker 删除伙伴安装关系，由伙伴聚合删除事务调用。
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
func (c *pluginController) FindSource(ctx context.Context, key string) (models.Plugin, error) {
	var p models.Plugin
	err := controllerDB(ctx, c.db).Where("source_key = ?", key).Limit(1).Find(&p).Error
	if err != nil {
		return p, databaseError("find_plugin_source", key, err)
	}
	return p, nil
}

// List 将全局发现和伙伴真实安装分开，统计不补齐展示资料。
func (c *pluginController) List(ctx context.Context, waker, query string, offset, limit int) ([]models.Plugin, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.Plugin{})
	if waker != "" {
		db = db.Where("id IN (SELECT plugin_id FROM waker_plugins WHERE waker_id = ?)", waker)
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
func (c *pluginController) Update(ctx context.Context, id, name, description string, categories *[]string) error {
	db := controllerDB(ctx, c.db)
	if categories != nil {
		if err := db.Model(&models.Plugin{}).Where("id = ?", id).Select("Categories").Updates(&models.Plugin{Categories: *categories}).Error; err != nil {
			return databaseError("update_plugin_categories", id, err)
		}
	}
	err := db.Model(&models.Plugin{}).Where("id = ?", id).Updates(map[string]any{"name": name, "description": description, "updated_at": nowMillis()}).Error
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
	if stored.Digest != v.Digest || v.CatalogBinding != "" && stored.CatalogBinding != "" && stored.CatalogBinding != v.CatalogBinding {
		return v, status_error.PluginVersionConflict
	}
	if v.CatalogBinding != "" && stored.ID != v.ID {
		// 只在可信目录完成同一固定版本的核验后刷新准备窗口和可重建快照。
		v.ID = stored.ID
		v.CreatedAt = stored.CreatedAt
		v.TrustedAt = max(v.TrustedAt, stored.TrustedAt)
		v.PreparedUntil = max(v.PreparedUntil, stored.PreparedUntil)
		if err := db.Model(&stored).Select("Package", "ContentPath", "Provenance", "CatalogBinding", "PreparedUntil", "TrustedAt", "Retired").Updates(&v).Error; err != nil {
			return v, databaseError("refresh_plugin_preparation", v.PluginID, err)
		}
		stored = v
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
	err := controllerDB(ctx, c.db).Where("plugin_id = ? AND retired = ?", id, false).Order("created_at DESC, id ASC").Find(&result).Error
	if err != nil {
		return nil, databaseError("list_plugin_versions", id, err)
	}
	return result, nil
}

// LatestVersion 读取无安装时的配置目标，不加载历史列表。
func (c *pluginController) LatestVersion(ctx context.Context, id string) (models.PluginVersion, error) {
	var version models.PluginVersion
	result := controllerDB(ctx, c.db).Where("plugin_id = ? AND retired = ?", id, false).Order("created_at DESC, id ASC").Limit(1).Find(&version)
	if result.Error != nil {
		return version, databaseError("latest_plugin_version", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return version, status_error.PluginNotFound
	}
	return version, nil
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

// Install 校验资源和版本，重复安装不重启停用关系。
func (c *pluginController) Install(ctx context.Context, r models.WakerPlugin) error {
	_, err := c.Get(ctx, r.PluginID)
	if err != nil {
		return err
	}
	v, err := c.Version(ctx, r.VersionID)
	if err != nil {
		return err
	}
	if v.PluginID != r.PluginID || v.Retired {
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
	if v.PluginID != plugin || v.Retired {
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

// SetRollbackVersion 更新插件一次可回滚的固定版本。
func (c *pluginController) SetRollbackVersion(ctx context.Context, plugin, version string) error {
	result := controllerDB(ctx, c.db).Model(&models.Plugin{}).Where("id = ?", plugin).UpdateColumn("rollback_version_id", version)
	if result.Error != nil {
		return databaseError("set_plugin_rollback_version", plugin, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.PluginNotFound
	}
	return nil
}

// Uninstall 移除指定伙伴关系，最后一个安装消失时清除回滚入口。
func (c *pluginController) Uninstall(ctx context.Context, waker, plugin string) error {
	db := controllerDB(ctx, c.db)
	if err := db.Where("waker_id = ? AND plugin_id = ?", waker, plugin).Delete(&models.WakerPlugin{}).Error; err != nil {
		return databaseError("uninstall_plugin", plugin, err)
	}
	if err := clearRollbackWithoutInstallations(db, []string{plugin}); err != nil {
		return err
	}
	publishReferenceChange(ctx, db, eventsResource("waker", waker))
	publishCommittedChange(ctx, db, constants.PluginEventTopic, eventsResource("plugin", plugin))
	return nil
}

// clearRollbackWithoutInstallations 在同一个写事务内解除已失去最后安装的版本引用。
func clearRollbackWithoutInstallations(db *gorm.DB, plugins []string) error {
	installed := db.Model(&models.WakerPlugin{}).Select("plugin_id").Where("plugin_id IN ?", plugins)
	err := db.Model(&models.Plugin{}).Where("id IN ? AND rollback_version_id <> ''", plugins).
		Where("id NOT IN (?)", installed).UpdateColumn("rollback_version_id", "").Error
	if err != nil {
		return databaseError("clear_unused_plugin_rollbacks", "", err)
	}
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
	config.Revision = expected + 1
	config.UpdatedAt = nowMillis()
	result := db.Model(&models.PluginConfiguration{}).Where("plugin_id = ? AND revision = ?", config.PluginID, expected).
		Select("Revision", "Values", "Secrets", "UpdatedAt").Updates(&config)
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
func (c *pluginController) Delete(ctx context.Context, id string) error {
	db := controllerDB(ctx, c.db)
	for _, model := range []any{&models.WakerPlugin{}, &models.PluginAccount{}, &models.PluginConfiguration{}, &models.PluginVersion{}} {
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

// DeleteByWaker 删除当前伙伴安装关系，保留共享资源与其他伙伴安装。
func (c *pluginController) DeleteByWaker(ctx context.Context, waker string) error {
	relations, err := c.Installations(ctx, "", waker)
	if err != nil {
		return err
	}
	db := controllerDB(ctx, c.db)
	if err := db.Where("waker_id = ?", waker).Delete(&models.WakerPlugin{}).Error; err != nil {
		return databaseError("delete_waker_plugins", waker, err)
	}
	plugins := make([]string, 0, len(relations))
	for _, relation := range relations {
		plugins = append(plugins, relation.PluginID)
		publishCommittedChange(ctx, db, constants.PluginEventTopic, eventsResource("plugin", relation.PluginID))
	}
	if len(plugins) > 0 {
		if err := clearRollbackWithoutInstallations(db, plugins); err != nil {
			return err
		}
	}
	publishReferenceChange(ctx, db, eventsResource("waker", waker))
	return nil
}

// ReadStates 在一个有界资源窗口批量读取关联事实，避免逐资源查询。
func (c *pluginController) ReadStates(ctx context.Context, ids []string) (map[string]models.PluginState, error) {
	return c.readStates(ctx, ids, false)
}

// ReadInstalledStates 读取伙伴列表所需的实际安装，不展开历史版本包。
func (c *pluginController) ReadInstalledStates(ctx context.Context, ids []string) (map[string]models.PluginState, error) {
	return c.readStates(ctx, ids, true)
}

func (c *pluginController) readStates(ctx context.Context, ids []string, installedOnly bool) (map[string]models.PluginState, error) {
	result := map[string]models.PluginState{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 100 {
		return nil, status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	var versions []models.PluginVersion
	query := db.Where("plugin_id IN ? AND retired = ?", ids, false)
	if installedOnly {
		installed := db.Model(&models.WakerPlugin{}).Where("plugin_id IN ?", ids).Select("version_id")
		query = query.Where("id IN (?)", installed).Select("id", "plugin_id", "version", "digest", "package", "created_at")
	}
	if err := query.Order("created_at DESC, id ASC").Find(&versions).Error; err != nil {
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
	if err := db.Select("plugin_id", "revision").Where("plugin_id IN ?", ids).Find(&configs).Error; err != nil {
		return nil, databaseError("read_plugin_configuration_revisions", "", err)
	}
	for _, config := range configs {
		item := result[config.PluginID]
		item.Configuration = models.PluginConfiguration{PluginID: config.PluginID, Revision: config.Revision}
		result[config.PluginID] = item
	}

	var accounts []models.PluginAccount
	if err := db.Select("plugin_id", "generation").Where("plugin_id IN ?", ids).Find(&accounts).Error; err != nil {
		return nil, databaseError("read_plugin_account_generations", "", err)
	}
	for _, account := range accounts {
		state := result[account.PluginID]
		state.Account = models.PluginAccount{PluginID: account.PluginID, Generation: account.Generation}
		result[account.PluginID] = state
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
		if err := tx.Select("id", "name").Where("id IN (?)", installed("plugin_id")).Find(&resources).Error; err != nil {
			return databaseError("execution_plugin_resources", waker, err)
		}
		var versions []models.PluginVersion
		if err := tx.Select("id", "plugin_id", "digest", "trusted_at", "content_path", "json_object('contractVersion', json_extract(package, '$.contractVersion'), 'manifest', json_extract(package, '$.manifest'), 'format', json_extract(package, '$.format'), 'sourcePath', json_extract(package, '$.sourcePath'), 'skills', json_extract(package, '$.skills'), 'servers', json_extract(package, '$.servers'), 'integration', json_extract(package, '$.integration')) AS package").Where("id IN (?)", installed("version_id")).Find(&versions).Error; err != nil {
			return databaseError("execution_plugin_versions", waker, err)
		}
		var configurations []models.PluginExecutionDeclaration
		if err := tx.Model(&models.PluginConfiguration{}).Select("plugin_id AS id", "revision AS configuration_revision", "values", "(SELECT json_group_array(json_extract(value, '$.name')) FROM json_each(COALESCE(NULLIF(plugin_configurations.secrets, ''), '[]')) WHERE json_extract(value, '$.configured') = 1) AS configured_secrets").Where("plugin_id IN (?)", installed("plugin_id")).Find(&configurations).Error; err != nil {
			return databaseError("execution_plugin_revisions", waker, err)
		}
		var accounts []models.PluginAccount
		if err := tx.Select("plugin_id", "generation").Where("plugin_id IN (?)", installed("plugin_id")).Find(&accounts).Error; err != nil {
			return databaseError("execution_plugin_accounts", waker, err)
		}
		generations := map[string]int64{}
		for _, account := range accounts {
			generations[account.PluginID] = account.Generation
		}
		byID := make(map[string]models.PluginVersion, len(versions))
		for _, version := range versions {
			byID[version.ID] = version
		}
		names := make(map[string]string, len(resources))
		for _, resource := range resources {
			names[resource.ID] = resource.Name
		}
		settings := make(map[string]models.PluginExecutionDeclaration, len(configurations))
		for _, configuration := range configurations {
			settings[configuration.ID] = configuration
		}
		for _, installation := range installations {
			version, exists := byID[installation.VersionID]
			name, owned := names[installation.PluginID]
			if !exists || !owned || version.PluginID != installation.PluginID {
				continue
			}
			configuration := settings[installation.PluginID]
			result = append(result, models.PluginExecutionDeclaration{ID: installation.PluginID, Name: name, VersionID: version.ID, Digest: version.Digest, TrustedAt: version.TrustedAt, ConfigurationRevision: configuration.ConfigurationRevision, AccountGeneration: generations[installation.PluginID], ContentPath: version.ContentPath, Package: version.Package, Values: configuration.Values, ConfiguredSecrets: configuration.ConfiguredSecrets})
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
	if err := db.Model(&models.PluginVersion{}).Where("retired = ? AND content_path <> ''", false).Pluck("content_path", &paths).Error; err != nil {
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

// RetireUnusedVersions 保留实际安装、上一回滚目标和仍在准备期的包。本地创建的技能包由资源本身持有。
func (c *pluginController) RetireUnusedVersions(ctx context.Context, active []string) error {
	db := controllerDB(ctx, c.db)
	now := nowMillis()
	installed := db.Model(&models.WakerPlugin{}).Select("version_id")
	installedPlugins := db.Model(&models.WakerPlugin{}).Select("plugin_id")
	rollbacks := db.Model(&models.Plugin{}).Select("rollback_version_id").Where("rollback_version_id <> '' AND id IN (?)", installedPlugins)
	authorized := db.Model(&models.PluginAccount{}).Select("authorized_version_id").Where("authorized_version_id <> ''")
	// 旧数据没有记录授权入口，每个资源保留最近的一个桥接版本供注销。
	knownAccounts := db.Model(&models.PluginAccount{}).Select("plugin_id").Where("authorized_version_id IS NOT NULL")
	legacy := db.Model(&models.Plugin{}).Where("id NOT IN (?)", knownAccounts).
		Select("(SELECT id FROM plugin_versions pv WHERE pv.plugin_id = plugins.id AND pv.retired = false AND json_type(pv.package, '$.integration.authentication') = 'object' ORDER BY pv.created_at DESC, pv.id ASC LIMIT 1) AS version_id")
	catalogs := db.Model(&models.Plugin{}).Select("id").Where("source_key LIKE ?", "catalog:%")
	candidates := db.Model(&models.PluginVersion{}).
		Where("plugin_id IN (?) AND retired = ?", catalogs, false).
		Where("id NOT IN (?) AND id NOT IN (?) AND id NOT IN (?)", installed, rollbacks, authorized).
		Where("NOT EXISTS (?)", db.Table("(?) AS legacy_auth", legacy).Where("legacy_auth.version_id = plugin_versions.id")).
		Where("(prepared_until BETWEEN 1 AND ?) OR (prepared_until = 0 AND created_at <= ?)", now, now-constants.PluginPreparationLifetime.Milliseconds())
	if len(active) > 0 {
		candidates = candidates.Where("content_path NOT IN ?", active)
	}
	result := candidates.Updates(map[string]any{"retired": true, "content_path": "", "package": "{}"})
	if result.Error != nil {
		return databaseError("retire_plugin_versions", "", result.Error)
	}
	return nil
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
	if err := db.Select("id", "name", "description").Where("id IN (?)", installed("plugin_id")).Order("name, id").Find(&resources).Error; err != nil {
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
