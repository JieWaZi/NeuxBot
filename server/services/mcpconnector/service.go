package mcpconnector

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Config 提供连接器写入的共享存储、验证与诊断边界。
type Config struct {
	// Connectors 保存共享配置和伙伴安装。
	Connectors controllers.MCPConnectorController
	// Wakers 校验伙伴仍存在。
	Wakers controllers.WakerController
	// Categories 保存资源分类。
	Categories controllers.ResourceCategoryController
	// Transactions 原子提交配置、安装和命令回执。
	Transactions controllers.TransactionController
	// Commands 保存聊天执行的变更回执。
	Commands capability.Store
	// Events 发布变更后的资源缓存失效。
	Events *events.Hub
	// Probe 是用于连接器诊断的实际协议调用。
	Probe func(context.Context, models.MCPConnector) mcpclient.Result
}

// Service 统一界面和聊天管理的连接器写入规则。
type Service struct {
	// config 持有连接器用例所需的依赖。
	config Config
}

// New 创建连接器管理服务。
func New(config Config) *Service {
	if config.Probe == nil {
		config.Probe = mcpclient.TestPinned
	}
	return &Service{config: config}
}

// Visible 读取当前伙伴安装的连接器；没有伙伴范围时只读取全局库。
func (s *Service) Visible(ctx context.Context, wakerID, connectorID string) (models.MCPConnectorWithState, error) {
	if wakerID == "" {
		connector, err := s.config.Connectors.Get(ctx, connectorID)
		if err != nil {
			return models.MCPConnectorWithState{}, err
		}
		if connector.OwnerWakerID != "" {
			return models.MCPConnectorWithState{}, status_error.MCPConnectorNotFound
		}
		return models.MCPConnectorWithState{Connector: connector}, nil
	}
	if _, err := s.config.Wakers.Get(ctx, wakerID); err != nil {
		return models.MCPConnectorWithState{}, err
	}
	return s.config.Connectors.GetForWaker(ctx, wakerID, connectorID)
}

// Create 保存受信任的连接器；伙伴自有的连接器同时安装给该伙伴。
func (s *Service) Create(ctx context.Context, wakerID string, input Definition) (models.MCPConnectorWithState, error) {
	item, err := newPublicConnector(input)
	if err != nil {
		return models.MCPConnectorWithState{}, err
	}
	if !input.TrustConfirmed {
		return models.MCPConnectorWithState{}, status_error.MCPConnectorTrustRequired
	}
	if wakerID != "" {
		item.OwnerWakerID = wakerID
		item.Scope = constants.MCPConnectorScopePrivate
	}
	return s.saveNew(ctx, wakerID, item, true)
}

func (s *Service) saveNew(ctx context.Context, wakerID string, connector models.MCPConnector, enabled bool) (models.MCPConnectorWithState, error) {
	connector.TrustedConfigDigest = trustDigest(connector)
	connector.TrustedAt = time.Now().UnixMilli()
	created := models.MCPConnectorWithState{Connector: connector, Enabled: wakerID != "" && enabled}
	err := s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		if wakerID != "" {
			if _, err := s.config.Wakers.Get(tx, wakerID); err != nil {
				return err
			}
		}
		if err := s.config.Connectors.Create(tx, connector); err != nil {
			return err
		}
		if err := s.ensureCategories(tx, connector); err != nil {
			return err
		}
		if wakerID != "" {
			if err := s.config.Connectors.UpsertWakerConnector(tx, models.WakerMCPConnector{
				ID: util.New(), WakerID: wakerID, MCPConnectorID: connector.ID, Enabled: created.Enabled,
				CreatedAt: connector.CreatedAt, UpdatedAt: connector.UpdatedAt,
			}); err != nil {
				return err
			}
		}
		return capability.Commit(tx, publicConnector(created))
	})
	if err == nil {
		PublishChange(ctx, s.config.Events, connector.ID)
	}
	return created, err
}

// Update 替换连接器的可编辑配置并检查用户确认。
func (s *Service) Update(ctx context.Context, wakerID, connectorID string, input Definition) (models.MCPConnectorWithState, error) {
	current, err := s.Visible(ctx, wakerID, connectorID)
	if err != nil {
		return models.MCPConnectorWithState{}, err
	}
	prepared, err := prepareConnector(current.Connector, input, &current.Connector)
	if err != nil {
		return models.MCPConnectorWithState{}, err
	}
	return s.saveUpdate(ctx, wakerID, current, prepared, input.TrustConfirmed, nil, nil)
}

func (s *Service) saveUpdate(ctx context.Context, wakerID string, current models.MCPConnectorWithState, prepared models.MCPConnector, trustConfirmed bool, approvedPartners []string, tested *mcpclient.Result) (models.MCPConnectorWithState, error) {
	existing := current.Connector
	if existing.Scope == constants.MCPConnectorScopeBuiltIn && builtInDefinitionChanged(existing, prepared) {
		return models.MCPConnectorWithState{}, status_error.BuiltInMCPConnectorReadOnly
	}
	now := time.Now().UnixMilli()
	digest := trustDigest(prepared)
	switch {
	case existing.Scope == constants.MCPConnectorScopeBuiltIn:
		prepared.TrustedConfigDigest = digest
	case digest != existing.TrustedConfigDigest:
		if !trustConfirmed {
			return models.MCPConnectorWithState{}, status_error.MCPConnectorTrustRequired
		}
		prepared.TrustedConfigDigest = digest
		prepared.TrustedAt = now
	}
	prepared.UpdatedAt = max(now, existing.UpdatedAt+1)
	configurationChanged := connectorConfigurationChanged(existing, prepared)
	runtimeChanged := configurationChanged || prepared.TrustedConfigDigest != existing.TrustedConfigDigest
	if configurationChanged {
		clearTestSnapshot(&prepared)
	}
	updated := models.MCPConnectorWithState{Connector: prepared, Enabled: current.Enabled}
	err := s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		fresh, err := s.Visible(tx, wakerID, existing.ID)
		if err != nil {
			return err
		}
		if fresh.Connector.UpdatedAt != existing.UpdatedAt {
			return status_error.Conflict
		}
		if capability.Active(tx) && runtimeChanged && fresh.Enabled != current.Enabled {
			return status_error.Conflict
		}
		if err := s.checkApprovedPartners(tx, existing.ID, approvedPartners); err != nil {
			return err
		}
		if err := s.ensureCategories(tx, prepared); err != nil {
			return err
		}
		toSave := prepared
		if tested != nil {
			applyTestSnapshot(&toSave, *tested)
		} else if !configurationChanged {
			toSave.TestStatus = fresh.Connector.TestStatus
			toSave.TestedAt = fresh.Connector.TestedAt
			toSave.TestDurationMS = fresh.Connector.TestDurationMS
			toSave.TestToolCount = fresh.Connector.TestToolCount
			toSave.TestError = fresh.Connector.TestError
			toSave.TestTools = fresh.Connector.TestTools
		}
		if err := s.config.Connectors.Update(tx, toSave); err != nil {
			return err
		}
		updated = models.MCPConnectorWithState{Connector: toSave, Enabled: fresh.Enabled}
		return capability.Commit(tx, publicConnector(updated))
	})
	if err == nil {
		PublishChange(ctx, s.config.Events, existing.ID)
	}
	return updated, err
}

// Remove 卸载当前伙伴的全局连接器，或删除其自有连接器。全局库删除校验确认时的安装数。
func (s *Service) Remove(ctx context.Context, wakerID, connectorID string, confirmed bool, expectedAffectedCount int64) error {
	current, err := s.Visible(ctx, wakerID, connectorID)
	if err != nil {
		return err
	}
	if wakerID == "" && !confirmed && current.Connector.Scope != constants.MCPConnectorScopeBuiltIn {
		return status_error.PluginConfigurationConflict
	}
	detach := current.Connector.OwnerWakerID == "" && wakerID != ""
	return s.remove(ctx, wakerID, current.Connector, detach, expectedAffectedCount, nil)
}

func (s *Service) remove(ctx context.Context, wakerID string, current models.MCPConnector, detach bool, expectedAffectedCount int64, approvedPartners []string) error {
	if current.Scope == constants.MCPConnectorScopeBuiltIn {
		return status_error.BuiltInMCPConnectorCannotDelete
	}
	updated := map[string]any{"connectorId": current.ID, "name": current.Name, "detached": detach, "deleted": !detach}
	err := s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		fresh, err := s.Visible(tx, wakerID, current.ID)
		if err != nil {
			return err
		}
		if fresh.Connector.UpdatedAt != current.UpdatedAt {
			return status_error.Conflict
		}
		if wakerID == "" {
			counts, err := s.config.Connectors.InstalledCounts(tx, []string{current.ID})
			if err != nil {
				return err
			}
			if counts[current.ID] != expectedAffectedCount {
				return status_error.PluginConfigurationConflict
			}
		}
		if err := s.checkApprovedPartners(tx, current.ID, approvedPartners); err != nil {
			return err
		}
		if detach {
			err = s.config.Connectors.DeleteWakerConnector(tx, wakerID, current.ID)
		} else {
			err = s.config.Connectors.DeleteConnector(tx, current.ID)
		}
		if err != nil {
			return err
		}
		return capability.Commit(tx, updated)
	})
	if err == nil {
		PublishChange(ctx, s.config.Events, current.ID)
	}
	return err
}

func (s *Service) checkApprovedPartners(ctx context.Context, connectorID string, approved []string) error {
	if approved == nil {
		return nil
	}
	partners, err := s.config.Connectors.InstalledPartnerIDs(ctx, connectorID)
	if err != nil {
		return err
	}
	if !slices.Equal(partners, approved) {
		return status_error.Conflict
	}
	return nil
}

func (s *Service) ensureCategories(ctx context.Context, connector models.MCPConnector) error {
	if len(connector.Categories) == 0 {
		return nil
	}
	return s.config.Categories.Ensure(ctx, constants.ResourceCategoryTypeConnector, connector.Categories)
}

// Test 运行协议级探测，并更新设置页面使用的诊断快照。
func (s *Service) Test(ctx context.Context, wakerID, connectorID string) (models.MCPConnectorWithState, error) {
	current, err := s.Visible(ctx, wakerID, connectorID)
	if err != nil {
		return models.MCPConnectorWithState{}, err
	}
	connector := current.Connector
	tested, err := s.probe(ctx, connector)
	if err != nil {
		return models.MCPConnectorWithState{}, err
	}
	applyTestSnapshot(&connector, tested)
	if err := s.config.Connectors.UpdateTestSnapshot(ctx, connector, connector.UpdatedAt); err != nil {
		return models.MCPConnectorWithState{}, err
	}
	PublishChange(ctx, s.config.Events, connector.ID)
	return models.MCPConnectorWithState{Connector: connector, Enabled: current.Enabled}, nil
}

func applyTestSnapshot(connector *models.MCPConnector, tested mcpclient.Result) {
	connector.TestStatus = tested.Status
	connector.TestedAt = time.Now().UnixMilli()
	connector.TestDurationMS = tested.DurationMS
	connector.TestError = bounded(tested.Error, constants.MCPConnectorMaximumSummaryLength)
	if tested.Status == constants.MCPTestStatusSucceeded {
		connector.TestTools = boundedTools(tested.Tools)
		connector.TestToolCount = len(tested.Tools)
	}
}

func (s *Service) probe(ctx context.Context, connector models.MCPConnector) (mcpclient.Result, error) {
	if !Trusted(connector) {
		return mcpclient.Result{}, status_error.MCPConnectorTrustRequired
	}
	return s.config.Probe(ctx, connector), nil
}

func bounded(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func boundedTools(tools []models.MCPToolSummary) []models.MCPToolSummary {
	limit := min(len(tools), constants.MCPConnectorMaximumToolCount)
	result := make([]models.MCPToolSummary, 0, limit)
	for _, tool := range tools[:limit] {
		if name := bounded(tool.Name, 200); name != "" {
			result = append(result, models.MCPToolSummary{Name: name, Description: bounded(tool.Description, constants.MCPConnectorMaximumSummaryLength)})
		}
	}
	return result
}

// PublishChange 通知资源消费者刷新已提交的连接器事实。
func PublishChange(ctx context.Context, hub *events.Hub, id string) {
	if hub == nil {
		return
	}
	resource := events.Resource{Kind: "connector", ID: id}
	if id == "" {
		resource = events.Resource{Kind: "global"}
	}
	events.AfterCommit(ctx, func() { _ = events.Emit(hub, "resources", "resources.changed", resource, resource) })
}
