package controllers

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MCPConnectorController 定义共享 MCP 连接器和伙伴关系的持久化操作。
type MCPConnectorController interface {
	// ListLibrary 分页查询全局连接器。
	ListLibrary(context.Context, string, int, int) ([]models.MCPConnectorWithState, int64, error)
	// InstalledCounts 返回实际安装关系总数。
	InstalledCounts(context.Context, []string) (map[string]int64, error)
	// Create 创建共享 MCP 连接器普通配置。
	Create(ctx context.Context, connector models.MCPConnector) error
	// Get 按标识查询共享 MCP 连接器。
	Get(ctx context.Context, connectorID string) (models.MCPConnector, error)
	// ListForWaker 返回全部连接器及当前伙伴的启用状态。
	ListForWaker(ctx context.Context, wakerID string, query string) ([]models.MCPConnectorWithState, error)
	// ListEnabled 返回当前伙伴已启用的连接器。
	ListEnabled(ctx context.Context, wakerID string) ([]models.MCPConnector, error)
	// Update 替换连接器的可编辑普通配置和调用方准备的测试字段。
	Update(ctx context.Context, connector models.MCPConnector) error
	// UpdateTestSnapshot 替换连接器最近一次测试快照。
	UpdateTestSnapshot(
		ctx context.Context,
		connector models.MCPConnector,
		expectedUpdatedAt int64,
	) error
	// UpsertWakerConnector 创建或更新伙伴连接器启停关系。
	UpsertWakerConnector(ctx context.Context, relation models.WakerMCPConnector) error
	// EnsureWakerConnector 幂等建立安装，不覆盖已有启停。
	EnsureWakerConnector(context.Context, models.WakerMCPConnector) error
	// DeleteWakerConnector 仅卸载一个伙伴的安装关系。
	DeleteWakerConnector(context.Context, string, string) error
	// DeleteByWaker 删除一个伙伴的全部连接器关系。
	DeleteByWaker(ctx context.Context, wakerID string) error
	// DeleteConnector 删除连接器和全部伙伴关系。
	DeleteConnector(ctx context.Context, connectorID string) error
}

// mcpConnectorController 使用 GORM 实现 MCP 连接器持久化。
type mcpConnectorController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewMCPConnectorController 创建 MCP 连接器 Repository Controller。
func NewMCPConnectorController(db *gorm.DB) MCPConnectorController {
	return &mcpConnectorController{db: db}
}

// Create 创建连接器并把全局名称冲突转换为稳定业务错误。
func (c *mcpConnectorController) Create(
	ctx context.Context,
	connector models.MCPConnector,
) error {
	db := controllerDB(ctx, c.db)
	if err := encryptConnectorSecrets(db, &connector); err != nil {
		return err
	}
	if err := db.Create(&connector).Error; err != nil {
		if dbhelper.IsDuplicateError(err) {
			return status_error.MCPConnectorNameConflict
		}
		return databaseError(
			"create_mcp_connector",
			"connector_id="+connector.ID,
			err,
		)
	}
	return nil
}

// Get 按标识查询一条连接器普通配置。
func (c *mcpConnectorController) Get(
	ctx context.Context,
	connectorID string,
) (models.MCPConnector, error) {
	var connector models.MCPConnector
	db := controllerDB(ctx, c.db)
	err := db.
		Where("id = ?", connectorID).
		First(&connector).Error
	if err == nil {
		normalizeConnectorCollections(&connector)
		if err := decryptConnectorSecrets(controllerDB(ctx, c.db), &connector); err != nil {
			return connector, err
		}
		return connector, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return connector, status_error.MCPConnectorNotFound.WithParams(
			map[string]interface{}{"ConnectorID": connectorID},
		)
	}
	return connector, databaseError(
		"get_mcp_connector",
		"connector_id="+connectorID,
		err,
	)
}

// ListForWaker 返回全部共享连接器及当前伙伴的显式或默认状态。
func (c *mcpConnectorController) ListForWaker(
	ctx context.Context,
	wakerID string,
	query string,
) ([]models.MCPConnectorWithState, error) {
	connectors := []models.MCPConnector{}
	db := controllerDB(ctx, c.db)
	if normalized := strings.TrimSpace(query); normalized != "" {
		pattern := "%" + strings.ToLower(normalized) + "%"
		db = db.Where(
			"LOWER(name) LIKE ? OR LOWER(description) LIKE ?",
			pattern,
			pattern,
		)
	}
	if err := db.
		Where("scope = ? OR owner_waker_id = ? OR (owner_waker_id = ? AND id IN (SELECT mcp_connector_id FROM waker_mcp_connectors WHERE waker_id = ?))", constants.MCPConnectorScopeBuiltIn, wakerID, "", wakerID).
		Order("CASE scope WHEN 'built_in' THEN 0 ELSE 1 END ASC").
		Order("updated_at DESC, id ASC").
		Find(&connectors).Error; err != nil {
		return nil, databaseError(
			"list_mcp_connectors",
			"waker_id="+wakerID,
			err,
		)
	}

	relations := []models.WakerMCPConnector{}
	if err := controllerDB(ctx, c.db).
		Where("waker_id = ?", wakerID).
		Find(&relations).Error; err != nil {
		return nil, databaseError(
			"list_waker_mcp_connectors",
			"waker_id="+wakerID,
			err,
		)
	}
	states := make(map[string]bool, len(relations))
	for _, relation := range relations {
		states[relation.MCPConnectorID] = relation.Enabled
	}
	result := make([]models.MCPConnectorWithState, 0, len(connectors))
	for _, connector := range connectors {
		normalizeConnectorCollections(&connector)
		if err := decryptConnectorSecrets(controllerDB(ctx, c.db), &connector); err != nil {
			return nil, err
		}
		enabled, hasRelation := states[connector.ID]
		if !hasRelation && connector.Scope == constants.MCPConnectorScopeBuiltIn {
			enabled = true
		}
		result = append(result, models.MCPConnectorWithState{
			Connector: connector,
			Enabled:   enabled,
		})
	}
	return result, nil
}

// ListEnabled 返回当前伙伴显式启用或按内置规则默认启用的连接器。
func (c *mcpConnectorController) ListEnabled(
	ctx context.Context,
	wakerID string,
) ([]models.MCPConnector, error) {
	items, err := c.ListForWaker(ctx, wakerID, "")
	if err != nil {
		return nil, err
	}
	result := make([]models.MCPConnector, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			result = append(result, item.Connector)
		}
	}
	return result, nil
}

// Update 替换连接器普通配置和调用方准备的测试字段。
func (c *mcpConnectorController) Update(
	ctx context.Context,
	connector models.MCPConnector,
) error {
	db := controllerDB(ctx, c.db)
	if err := encryptConnectorSecrets(db, &connector); err != nil {
		return err
	}
	result := db.
		Model(&models.MCPConnector{}).
		Where("id = ?", connector.ID).
		Select(
			"Name",
			"Description",
			"Transport",
			"Command",
			"Arguments",
			"Environment",
			"URL",
			"Headers",
			"TimeoutMS",
			"TrustedConfigDigest",
			"TrustedAt",
			"TestStatus",
			"TestedAt",
			"TestDurationMS",
			"TestToolCount",
			"TestError",
			"TestTools",
			"UpdatedAt",
		).
		Updates(&connector)
	if result.Error != nil {
		if dbhelper.IsDuplicateError(result.Error) {
			return status_error.MCPConnectorNameConflict
		}
		return databaseError(
			"update_mcp_connector",
			"connector_id="+connector.ID,
			result.Error,
		)
	}
	if result.RowsAffected == 0 {
		return status_error.MCPConnectorNotFound.WithParams(
			map[string]interface{}{"ConnectorID": connector.ID},
		)
	}
	return nil
}

// UpdateTestSnapshot 替换最近一次测试的有界结果。
func (c *mcpConnectorController) UpdateTestSnapshot(
	ctx context.Context,
	connector models.MCPConnector,
	expectedUpdatedAt int64,
) error {
	result := controllerDB(ctx, c.db).
		Model(&models.MCPConnector{}).
		Where("id = ? AND updated_at = ?", connector.ID, expectedUpdatedAt).
		Select(
			"TestStatus",
			"TestedAt",
			"TestDurationMS",
			"TestToolCount",
			"TestError",
			"TestTools",
		).
		Updates(&connector)
	if result.Error != nil {
		return databaseError(
			"update_mcp_test_snapshot",
			"connector_id="+connector.ID,
			result.Error,
		)
	}
	if result.RowsAffected == 0 {
		return status_error.Conflict
	}
	return nil
}

// UpsertWakerConnector 原子创建或更新伙伴启停关系。
func (c *mcpConnectorController) UpsertWakerConnector(
	ctx context.Context,
	relation models.WakerMCPConnector,
) error {
	connector, err := c.Get(ctx, relation.MCPConnectorID)
	if err != nil {
		return err
	}
	if connector.OwnerWakerID != "" && connector.OwnerWakerID != relation.WakerID {
		return status_error.MCPConnectorNotFound
	}
	err = controllerDB(ctx, c.db).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "waker_id"},
				{Name: "mcp_connector_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"enabled":    relation.Enabled,
				"updated_at": relation.UpdatedAt,
			}),
		}).
		Create(&relation).Error
	if err != nil {
		return databaseError(
			"upsert_waker_mcp_connector",
			"waker_id="+relation.WakerID,
			err,
		)
	}
	return nil
}

// DeleteByWaker 删除一个伙伴的全部连接器关系。
func (c *mcpConnectorController) DeleteByWaker(
	ctx context.Context,
	wakerID string,
) error {
	var owned []models.MCPConnector
	if err := controllerDB(ctx, c.db).Where("owner_waker_id = ?", wakerID).Find(&owned).Error; err != nil {
		return databaseError("list_owned_connectors", wakerID, err)
	}
	for _, connector := range owned {
		if err := c.DeleteConnector(ctx, connector.ID); err != nil {
			return err
		}
	}
	err := controllerDB(ctx, c.db).
		Where("waker_id = ?", wakerID).
		Delete(&models.WakerMCPConnector{}).Error
	if err != nil {
		return databaseError(
			"delete_waker_mcp_connectors",
			"waker_id="+wakerID,
			err,
		)
	}
	return nil
}

// DeleteConnector 删除一个连接器及全部伙伴关系。
func (c *mcpConnectorController) DeleteConnector(
	ctx context.Context,
	connectorID string,
) error {
	db := controllerDB(ctx, c.db)
	if err := db.
		Where("mcp_connector_id = ?", connectorID).
		Delete(&models.WakerMCPConnector{}).Error; err != nil {
		return databaseError(
			"delete_mcp_connector_relations",
			"connector_id="+connectorID,
			err,
		)
	}
	result := db.Where("id = ?", connectorID).Delete(&models.MCPConnector{})
	if result.Error != nil {
		return databaseError(
			"delete_mcp_connector",
			"connector_id="+connectorID,
			result.Error,
		)
	}
	if result.RowsAffected == 0 {
		return status_error.MCPConnectorNotFound.WithParams(
			map[string]interface{}{"ConnectorID": connectorID},
		)
	}
	return nil
}

func normalizeConnectorCollections(connector *models.MCPConnector) {
	if connector.Arguments == nil {
		connector.Arguments = []string{}
	}
	if connector.Environment == nil {
		connector.Environment = []models.MCPSecretDefinition{}
	}
	if connector.Headers == nil {
		connector.Headers = []models.MCPSecretDefinition{}
	}
	if connector.TestTools == nil {
		connector.TestTools = []models.MCPToolSummary{}
	}
}

// DeleteWakerConnector 只删除指定伙伴安装，不删除共享配置和账号。
func (c *mcpConnectorController) DeleteWakerConnector(ctx context.Context, waker, connector string) error {
	err := controllerDB(ctx, c.db).Where("waker_id = ? AND mcp_connector_id = ?", waker, connector).Delete(&models.WakerMCPConnector{}).Error
	if err != nil {
		return databaseError("uninstall_waker_connector", connector, err)
	}
	return nil
}

// ListLibrary 分页查询全局连接器，敏感值只在 Controller 内解密使用。
func (c *mcpConnectorController) ListLibrary(ctx context.Context, query string, offset, limit int) ([]models.MCPConnectorWithState, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.MCPConnector{}).Where("owner_waker_id = ?", "")
	if query = strings.TrimSpace(query); query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		db = db.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ?", pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_connector_library", "", err)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var connectors []models.MCPConnector
	if err := db.Order("updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&connectors).Error; err != nil {
		return nil, 0, databaseError("list_connector_library", "", err)
	}
	result := make([]models.MCPConnectorWithState, 0, len(connectors))
	for _, connector := range connectors {
		normalizeConnectorCollections(&connector)
		if err := decryptConnectorSecrets(controllerDB(ctx, c.db), &connector); err != nil {
			return nil, 0, err
		}
		result = append(result, models.MCPConnectorWithState{Connector: connector})
	}
	return result, total, nil
}

// InstalledCounts 批量读取实际伙伴安装数量。
func (c *mcpConnectorController) InstalledCounts(ctx context.Context, ids []string) (map[string]int64, error) {
	return installedResourceCounts(ctx, c.db, &models.WakerMCPConnector{}, "mcp_connector_id", ids)
}

// EnsureWakerConnector 用唯一关系身份防止重复安装重置配置。
func (c *mcpConnectorController) EnsureWakerConnector(ctx context.Context, relation models.WakerMCPConnector) error {
	connector, err := c.Get(ctx, relation.MCPConnectorID)
	if err != nil {
		return err
	}
	if connector.OwnerWakerID != "" && connector.OwnerWakerID != relation.WakerID {
		return status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&relation).Error; err != nil {
		return databaseError("ensure_waker_connector", relation.WakerID, err)
	}
	return nil
}
