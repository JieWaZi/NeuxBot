package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IMController 持久保存连接、聊天授权、耐久收件与独立投递事实。
type IMController interface {
	// Connections 枚举平台连接，不返回解密凭据。
	Connections(context.Context) ([]models.IMConnection, error)
	// Connection 读取后台适配器需要的进程内凭据。
	Connection(context.Context, string) (models.IMConnection, error)
	// SaveConnection 按版本保存配置并加密新的凭据。
	SaveConnection(context.Context, models.IMConnection, int64) error
	// DeleteConnection 按版本删除连接、凭据及 IM 关联，保留内部对话。
	DeleteConnection(context.Context, string, int64, string) error
	// ConnectionDependencies 查询删除连接将影响的当前版本、活跃运行及待发送消息。
	ConnectionDependencies(context.Context, string) (models.IMConnectionDependencies, error)
	// ConnectionState 更新运行事实，不改变管理员配置版本。
	ConnectionState(context.Context, string, constants.IMConnectionState, string) error
	// SaveCursor 在收件事务中更新平台批次游标。
	SaveCursor(context.Context, string, string) error
	// Bindings 分页枚举已发现的聊天和授权状态。
	Bindings(context.Context, string, int, int) ([]models.IMBinding, int64, error)
	// Binding 返回指定聊天授权及进程内微信发送上下文。
	Binding(context.Context, string) (models.IMBinding, error)
	// DiscoverBinding 幂等发现聊天并刷新加密发送上下文，不改变授权。
	DiscoverBinding(context.Context, models.IMBinding) error
	// SaveBinding 按版本保存管理员配置和当前对话指针。
	SaveBinding(context.Context, models.IMBinding, int64) error
	// SaveInbox 以平台消息身份去重提交输入。
	SaveInbox(context.Context, models.IMInbox) error
	// Inbox 枚举耐久输入，限量返回避免后台无界扫描。
	Inbox(context.Context, constants.IMInboxState, int) ([]models.IMInbox, error)
	// QuoteLink 只在同一授权聊天内定位被引用的输入或已发送消息。
	QuoteLink(context.Context, string, string) (string, error)
	// UpdateInbox 更新已冻结的路由和接纳事实。
	UpdateInbox(context.Context, models.IMInbox) error
	// Links 枚举一个授权聊天内的历史会话，空绑定仅供后台恢复。
	Links(context.Context, string) ([]models.IMConversationLink, error)
	// Link 读取冻结的会话配置。
	Link(context.Context, string) (models.IMConversationLink, error)
	// SaveLink 建立预分配会话，不创建内部空 Chat。
	SaveLink(context.Context, models.IMConversationLink) error
	// CheckpointLink 提交安全投影游标和仍待决定的报告，不修改实际对话关系。
	CheckpointLink(context.Context, models.IMConversationLink) error
	// UpdateLink 只绑定真实内部关系，不覆盖后台投影游标。
	UpdateLink(context.Context, models.IMConversationLink) error
	// CreateDelivery 依靠唯一请求身份提交一份冻结投递。
	CreateDelivery(context.Context, models.IMDelivery) error
	// Delivery 读取单份完整投递。
	Delivery(context.Context, string) (models.IMDelivery, error)
	// FindDelivery 对账调用者稳定请求身份。
	FindDelivery(context.Context, string) (models.IMDelivery, bool, error)
	// Deliveries 分页列出发送事实或后台待发记录。
	Deliveries(context.Context, constants.IMDeliveryState, int, int) ([]models.IMDelivery, int64, error)
	// ClaimDelivery 只允许一次发送者领取仍等待的记录。
	ClaimDelivery(context.Context, string) (bool, error)
	// UpdateDelivery 保存片段收据和最终发送状态。
	UpdateDelivery(context.Context, models.IMDelivery) error
	// RecoverDeliveries 将异常退出时未确认的发送标记为结果不明。
	RecoverDeliveries(context.Context) error
}

// imController 在调用者事务内持久保存 IM 聚合。
type imController struct {
	// db 是未注入事务时使用的默认连接。
	db *gorm.DB
}

// NewIMController 建立 IM 持久化边界。
func NewIMController(db *gorm.DB) IMController { return &imController{db: db} }

// Connections 枚举平台连接，不返回解密凭据。
func (c *imController) Connections(ctx context.Context) ([]models.IMConnection, error) {
	result := []models.IMConnection{}
	db := controllerDB(ctx, c.db)
	counts := db.Table("im_conversation_links AS links").
		Select("COUNT(*)").
		Where("links.binding_id IN (SELECT id FROM im_bindings WHERE connection_id = im_connections.id)").
		Where("EXISTS (SELECT 1 FROM chats WHERE id = links.chat_id)")
	err := db.Select("im_connections.*, (?) AS conversation_count", counts).
		Order("created_at ASC").Find(&result).Error
	if err != nil {
		return nil, databaseError("list_im_connections", "", err)
	}
	return result, nil
}

// Connection 读取后台适配器需要的进程内凭据。
func (c *imController) Connection(ctx context.Context, id string) (models.IMConnection, error) {
	var result models.IMConnection
	db := controllerDB(ctx, c.db)
	if err := db.Where("id = ?", id).First(&result).Error; err != nil {
		return result, imReadError("get_im_connection", id, err)
	}
	if result.CredentialCiphertext != "" {
		value, err := c.decrypt(db, result.CredentialCiphertext, id+"/credential")
		if err != nil {
			return result, err
		}
		result.Credential = value
	}
	return result, nil
}

// SaveConnection 按版本保存配置并加密新的凭据。
func (c *imController) SaveConnection(ctx context.Context, value models.IMConnection, revision int64) error {
	db := controllerDB(ctx, c.db)
	if value.Credential != "" {
		cipher, err := c.encrypt(db, value.Credential, value.ID+"/credential")
		if err != nil {
			return err
		}
		value.CredentialCiphertext = cipher
	}
	value.Revision = revision + 1
	if revision == 0 {
		if err := db.Create(&value).Error; err != nil {
			return databaseError("create_im_connection", value.ID, err)
		}
		publishCommittedChange(ctx, c.db, "im", eventsResource("connection", value.ID))
		return nil
	}
	result := db.Model(&models.IMConnection{}).Where("id = ? AND revision = ? AND (app_id = '' OR app_id = ?) AND (owner_id = '' OR owner_id = ?)", value.ID, revision, value.AppID, value.OwnerID).Updates(map[string]any{"name": value.Name, "enabled": value.Enabled, "keep_awake": value.KeepAwake, "app_id": value.AppID, "owner_id": value.OwnerID, "base_url": value.BaseURL, "credential_ciphertext": value.CredentialCiphertext, "revision": value.Revision})
	if result.Error != nil {
		return databaseError("update_im_connection", value.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.IMConfigurationConflict
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("connection", value.ID))
	return nil
}

// DeleteConnection 在调用者事务中清理连接聚合，不删除内部 Chat 或团队线程。
func (c *imController) DeleteConnection(ctx context.Context, id string, revision int64, confirmedDigest string) error {
	db := controllerDB(ctx, c.db)
	impact, err := c.ConnectionDependencies(ctx, id)
	if err != nil {
		return err
	}
	if impact.PendingDeliveries > 0 {
		return status_error.IMConnectionInUse
	}
	for _, reference := range impact.Workflows {
		if reference.RunID != "" {
			return status_error.IMConnectionInUse.WithParams(map[string]any{"dependencies": impact})
		}
	}
	if len(impact.Workflows) > 0 && confirmedDigest != impact.Digest {
		return status_error.IMConnectionHasWorkflowDependencies.WithParams(map[string]any{"dependencies": impact})
	}
	result := db.Where("id = ? AND revision = ?", id, revision).Delete(&models.IMConnection{})
	if result.Error != nil {
		return databaseError("delete_im_connection", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.IMConfigurationConflict
	}
	bindings := db.Model(&models.IMBinding{}).Select("id").Where("connection_id = ?", id)
	if err := db.Where("binding_id IN (?)", bindings).Delete(&models.IMConversationLink{}).Error; err != nil {
		return databaseError("delete_im_links", id, err)
	}
	for _, model := range []any{&models.IMInbox{}, &models.IMDelivery{}, &models.IMBinding{}} {
		if err := db.Where("connection_id = ?", id).Delete(model).Error; err != nil {
			return databaseError("delete_im_association", id, err)
		}
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("connection", id))
	return nil
}

// ConnectionDependencies 从不可变 Go 程序识别真实发送目标；历史版本不占用可删除资源。
func (c *imController) ConnectionDependencies(ctx context.Context, id string) (models.IMConnectionDependencies, error) {
	result := models.IMConnectionDependencies{Workflows: []models.IMWorkflowDependency{}}
	if _, err := c.Connection(ctx, id); err != nil {
		return result, err
	}
	db := controllerDB(ctx, c.db)
	var bindings []models.IMBinding
	if err := db.Select("id").Where("connection_id = ?", id).Find(&bindings).Error; err != nil {
		return result, databaseError("connection_dependency_targets", id, err)
	}
	targets := map[string]bool{}
	for _, binding := range bindings {
		targets[binding.ID] = true
	}
	// 只分批读取当前定义及活跃运行引用的版本；历史源码不会进入内存。
	activeStatuses := []constants.WorkflowRunStatus{constants.WorkflowRunStatusQueued, constants.WorkflowRunStatusRunning, constants.WorkflowRunStatusWaiting, constants.WorkflowRunStatusRecovering, constants.WorkflowRunStatusCancelling}
	current := db.Model(&models.WorkflowDefinition{}).Select("current_revision_id")
	active := db.Model(&models.WorkflowRun{}).Select("revision_id").Where("status IN ?", activeStatuses)
	query := db.Model(&models.WorkflowRevision{}).Select("id,definition_id,manifest,bundle").Where("id IN (?) OR id IN (?)", current, active).Where("EXISTS (SELECT 1 FROM workflow_definitions WHERE id=workflow_revisions.definition_id)")
	if len(targets) > 0 {
		err := walkRepositoryRows(query, func(revisions []models.WorkflowRevision) error {
			dependencies, err := c.revisionIMTargetsBatch(ctx, revisions)
			if err != nil {
				return err
			}
			definitionIDs, revisionIDs := []string{}, []string{}
			matching := map[string][]workflowdsl.IMTargetDependency{}
			for _, revision := range revisions {
				seen := map[string]bool{}
				for _, dependency := range dependencies[revision.ID] {
					if targets[dependency.TargetID] && !seen[dependency.NodeID] {
						matching[revision.ID] = append(matching[revision.ID], dependency)
						seen[dependency.NodeID] = true
					}
				}
				if len(matching[revision.ID]) > 0 {
					definitionIDs = append(definitionIDs, revision.DefinitionID)
					revisionIDs = append(revisionIDs, revision.ID)
				}
			}
			definitions, err := readRelatedRows[models.WorkflowDefinition](db, "id", "id,title,current_revision_id", definitionIDs)
			if err != nil {
				return err
			}
			definitionByID := map[string]models.WorkflowDefinition{}
			for _, definition := range definitions {
				definitionByID[definition.ID] = definition
			}
			byRevision := map[string]models.WorkflowRevision{}
			for _, revision := range revisions {
				byRevision[revision.ID] = models.WorkflowRevision{ID: revision.ID, DefinitionID: revision.DefinitionID}
			}
			appendDependencies := func(revisionID, runID string) {
				revision := byRevision[revisionID]
				definition := definitionByID[revision.DefinitionID]
				for _, dependency := range matching[revisionID] {
					result.Workflows = append(result.Workflows, models.IMWorkflowDependency{DefinitionID: revision.DefinitionID, RevisionID: revision.ID, Title: definition.Title, NodeID: dependency.NodeID, Label: dependency.Label, RunID: runID})
				}
			}
			activeRevisions := map[string]bool{}
			if len(revisionIDs) > 0 {
				if err := walkRepositoryRows(db.Model(&models.WorkflowRun{}).Select("id,revision_id").Where("revision_id IN ? AND status IN ?", revisionIDs, activeStatuses), func(runs []models.WorkflowRun) error {
					for _, run := range runs {
						activeRevisions[run.RevisionID] = true
						appendDependencies(run.RevisionID, run.ID)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			for _, revision := range revisions {
				if !activeRevisions[revision.ID] && definitionByID[revision.DefinitionID].CurrentRevisionID == revision.ID {
					appendDependencies(revision.ID, "")
				}
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	sort.Slice(result.Workflows, func(i, j int) bool {
		a, b := result.Workflows[i], result.Workflows[j]
		return a.DefinitionID+a.RevisionID+a.NodeID+a.RunID < b.DefinitionID+b.RevisionID+b.NodeID+b.RunID
	})
	if err := db.Model(&models.IMDelivery{}).Where("connection_id = ? AND state IN ?", id, []constants.IMDeliveryState{constants.IMDeliveryStatePending, constants.IMDeliveryStateSending}).Count(&result.PendingDeliveries).Error; err != nil {
		return result, databaseError("connection_pending_deliveries", id, err)
	}
	identity, err := requestidempotency.New(id, []any{id, result.Workflows})
	result.Digest = identity.Hash
	return result, err
}

// imDependencyNode 只保存解析后的资源引用，不缓存工作流源码和执行程序。
type imDependencyNode struct {
	// DefinitionID 用于拒绝跨定义引用不相干版本。
	DefinitionID string
	// Targets 是本版本直接引用的 IM 目标。
	Targets []workflowdsl.IMTargetDependency
	// Children 是需要继续解析的子工作流引用。
	Children []workflowdsl.ChildWorkflowDependency
}

// revisionIMTargetsBatch 按依赖层批量读取子版本；递归归并阶段不再访问数据库。
func (c *imController) revisionIMTargetsBatch(ctx context.Context, roots []models.WorkflowRevision) (map[string][]workflowdsl.IMTargetDependency, error) {
	db := controllerDB(ctx, c.db)
	nodes := map[string]imDependencyNode{}
	definitions := map[string]models.WorkflowDefinition{}
	seenDefinitions, seenRevisions := map[string]bool{}, map[string]bool{}
	parse := func(revision models.WorkflowRevision) error {
		targets, err := workflowdsl.IMTargetDependencies(revision.Manifest, revision.Bundle)
		if err != nil {
			return err
		}
		nodes[revision.ID] = imDependencyNode{DefinitionID: revision.DefinitionID, Targets: targets, Children: workflowdsl.ChildWorkflowDependencies(revision.Manifest, revision.Bundle)}
		seenRevisions[revision.ID] = true
		return nil
	}
	frontier := []string{}
	for _, revision := range roots {
		if err := parse(revision); err != nil {
			return nil, err
		}
		frontier = append(frontier, revision.ID)
	}
	for len(frontier) > 0 {
		definitionIDs := []string{}
		for _, id := range frontier {
			for _, child := range nodes[id].Children {
				if !seenDefinitions[child.DefinitionID] {
					seenDefinitions[child.DefinitionID] = true
					definitionIDs = append(definitionIDs, child.DefinitionID)
				}
			}
		}
		rows, err := readRelatedRows[models.WorkflowDefinition](db, "id", "id,current_revision_id", definitionIDs)
		if err != nil {
			return nil, err
		}
		for _, definition := range rows {
			definitions[definition.ID] = definition
		}
		revisionIDs := []string{}
		for _, id := range frontier {
			node := nodes[id]
			for i := range node.Children {
				child := &node.Children[i]
				definition, ok := definitions[child.DefinitionID]
				if !ok {
					continue
				}
				if child.RevisionID == "" {
					child.RevisionID = definition.CurrentRevisionID
				}
				if child.RevisionID != "" && !seenRevisions[child.RevisionID] {
					seenRevisions[child.RevisionID] = true
					revisionIDs = append(revisionIDs, child.RevisionID)
				}
			}
			nodes[id] = node
		}
		frontier = nil
		for start := 0; start < len(revisionIDs); start += repositoryReadBatch {
			revisions, err := readRelatedRows[models.WorkflowRevision](db, "id", "id,definition_id,manifest,bundle", revisionIDs[start:min(start+repositoryReadBatch, len(revisionIDs))])
			if err != nil {
				return nil, err
			}
			for _, revision := range revisions {
				if err := parse(revision); err != nil {
					return nil, err
				}
				frontier = append(frontier, revision.ID)
			}
		}
	}
	var collect func(string, map[string]bool) []workflowdsl.IMTargetDependency
	collect = func(id string, path map[string]bool) []workflowdsl.IMTargetDependency {
		if path[id] {
			return nil
		}
		path[id] = true
		defer delete(path, id)
		node := nodes[id]
		result := append([]workflowdsl.IMTargetDependency{}, node.Targets...)
		for _, child := range node.Children {
			if _, exists := definitions[child.DefinitionID]; !exists || nodes[child.RevisionID].DefinitionID != child.DefinitionID {
				continue
			}
			for _, dependency := range collect(child.RevisionID, path) {
				dependency.NodeID, dependency.Label, dependency.Range = child.NodeID, child.Label, child.Range
				result = append(result, dependency)
			}
		}
		return result
	}
	result := map[string][]workflowdsl.IMTargetDependency{}
	for _, root := range roots {
		result[root.ID] = collect(root.ID, map[string]bool{})
	}
	return result, nil
}

// ConnectionState 更新运行事实，不改变管理员配置版本。
func (c *imController) ConnectionState(ctx context.Context, id string, state constants.IMConnectionState, code string) error {
	err := controllerDB(ctx, c.db).Model(&models.IMConnection{}).Where("id = ?", id).Updates(map[string]any{"state": state, "error_code": code}).Error
	if err != nil {
		return databaseError("update_im_connection_state", id, err)
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("connection", id))
	return nil
}

// SaveCursor 在收件事务中更新平台批次游标。
func (c *imController) SaveCursor(ctx context.Context, id, cursor string) error {
	err := controllerDB(ctx, c.db).Model(&models.IMConnection{}).Where("id = ?", id).Update("cursor", cursor).Error
	if err != nil {
		return databaseError("save_im_cursor", id, err)
	}
	return nil
}

// Bindings 分页枚举已发现的聊天和授权状态。
func (c *imController) Bindings(ctx context.Context, connectionID string, limit, offset int) ([]models.IMBinding, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.IMBinding{})
	if connectionID != "" {
		db = db.Where("connection_id = ?", connectionID)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, databaseError("count_im_bindings", "", err)
	}
	result := []models.IMBinding{}
	if limit > 0 {
		db = db.Limit(limit).Offset(offset)
	}
	if err := db.Order("created_at DESC, id ASC").Find(&result).Error; err != nil {
		return nil, 0, databaseError("list_im_bindings", "", err)
	}
	return result, count, nil
}

// Binding 返回指定聊天授权及进程内微信发送上下文。
func (c *imController) Binding(ctx context.Context, id string) (models.IMBinding, error) {
	var result models.IMBinding
	db := controllerDB(ctx, c.db)
	if err := db.Where("id = ?", id).First(&result).Error; err != nil {
		return result, imReadError("get_im_binding", id, err)
	}
	if result.ContextCiphertext != "" {
		value, err := c.decrypt(db, result.ContextCiphertext, id+"/context")
		if err != nil {
			return result, err
		}
		result.ContextToken = value
	}
	return result, nil
}

// DiscoverBinding 幂等发现聊天并刷新加密发送上下文，不改变授权。
func (c *imController) DiscoverBinding(ctx context.Context, value models.IMBinding) error {
	db := controllerDB(ctx, c.db)
	if value.ContextToken != "" {
		cipher, err := c.encrypt(db, value.ContextToken, value.ID+"/context")
		if err != nil {
			return err
		}
		value.ContextCiphertext = cipher
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&value).Error; err != nil {
		return databaseError("discover_im_binding", value.ID, err)
	}
	if value.ContextCiphertext != "" {
		if err := db.Model(&models.IMBinding{}).Where("id = ?", value.ID).Update("context_ciphertext", value.ContextCiphertext).Error; err != nil {
			return databaseError("refresh_im_context", value.ID, err)
		}
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("global", ""))
	return nil
}

// SaveBinding 按版本保存管理员配置和当前对话指针。
func (c *imController) SaveBinding(ctx context.Context, value models.IMBinding, revision int64) error {
	value.Revision = revision + 1
	result := controllerDB(ctx, c.db).Model(&models.IMBinding{}).Where("id = ? AND revision = ?", value.ID, revision).Select("name", "state", "waker_id", "channel_id", "project_id", "working_directory", "options", "active_link_id", "controller_ids", "allow_workflow_delivery", "revision", "updated_at").Updates(&value)
	if result.Error != nil {
		return databaseError("save_im_binding", value.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.IMConfigurationConflict
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("global", ""))
	return nil
}

// SaveInbox 以平台消息身份去重提交输入。
func (c *imController) SaveInbox(ctx context.Context, value models.IMInbox) error {
	if err := c.encryptFiles(ctx, &value); err != nil {
		return err
	}
	if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&value).Error; err != nil {
		return databaseError("save_im_inbox", value.ID, err)
	}
	return nil
}

// Inbox 枚举耐久输入，限量返回避免后台无界扫描。
func (c *imController) Inbox(ctx context.Context, state constants.IMInboxState, limit int) ([]models.IMInbox, error) {
	result := []models.IMInbox{}
	db := controllerDB(ctx, c.db).Where("state = ?", state)
	if state == constants.IMInboxStatePending {
		db = db.Where("EXISTS (SELECT 1 FROM im_bindings b WHERE b.id=im_inbox.binding_id AND b.state!=? AND EXISTS (SELECT 1 FROM im_connections WHERE id=b.connection_id AND enabled=1))", constants.IMBindingStatePending)
	}
	if state == constants.IMInboxStateRouted {
		db = db.Where("EXISTS (SELECT 1 FROM im_connections c WHERE c.id=im_inbox.connection_id AND c.enabled=1)").Where("content LIKE '/%' OR NOT EXISTS (SELECT 1 FROM im_inbox older WHERE older.state=? AND older.link_id=im_inbox.link_id AND older.rowid<im_inbox.rowid AND older.content NOT LIKE '/%')", state)
	}
	err := db.Order("rowid ASC").Limit(limit).Find(&result).Error
	if err != nil {
		return nil, databaseError("list_im_inbox", "", err)
	}
	for i := range result {
		if result[i].FilesCiphertext != "" {
			plain, err := c.decrypt(controllerDB(ctx, c.db), result[i].FilesCiphertext, result[i].ID+"/files")
			if err != nil {
				return nil, err
			}
			if json.Unmarshal([]byte(plain), &result[i].Files) != nil {
				return nil, status_error.IMCredentialError
			}
		}
	}
	return result, nil
}

// UpdateInbox 更新已冻结的路由和接纳事实。
func (c *imController) UpdateInbox(ctx context.Context, value models.IMInbox) error {
	if err := c.encryptFiles(ctx, &value); err != nil {
		return err
	}
	result := controllerDB(ctx, c.db).Model(&models.IMInbox{}).Where("id = ?", value.ID).Select("state", "link_id", "execution_id", "error_code", "files_ciphertext").Updates(&value)
	if result.Error != nil {
		return databaseError("update_im_inbox", value.ID, result.Error)
	}
	return nil
}

// Links 枚举一个授权聊天内的历史会话，空绑定仅供后台恢复。
func (c *imController) Links(ctx context.Context, bindingID string) ([]models.IMConversationLink, error) {
	result := []models.IMConversationLink{}
	db := controllerDB(ctx, c.db)
	if bindingID != "" {
		db = db.Where("binding_id = ?", bindingID)
	}
	if err := db.Order("created_at DESC, id ASC").Find(&result).Error; err != nil {
		return nil, databaseError("list_im_links", bindingID, err)
	}
	return result, nil
}

// Link 读取冻结的会话配置。
func (c *imController) Link(ctx context.Context, id string) (models.IMConversationLink, error) {
	var result models.IMConversationLink
	if err := controllerDB(ctx, c.db).Where("id = ?", id).First(&result).Error; err != nil {
		return result, imReadError("get_im_link", id, err)
	}
	return result, nil
}

// SaveLink 建立预分配会话，不创建内部空 Chat。
func (c *imController) SaveLink(ctx context.Context, value models.IMConversationLink) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_im_link", value.ID, err)
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("global", ""))
	return nil
}

// UpdateLink 提交内部关系或安全事件投影游标。
func (c *imController) UpdateLink(ctx context.Context, value models.IMConversationLink) error {
	if err := controllerDB(ctx, c.db).Model(&models.IMConversationLink{}).Where("id = ?", value.ID).Select("chat_id", "thread_id").Updates(&value).Error; err != nil {
		return databaseError("update_im_link", value.ID, err)
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("global", ""))
	return nil
}

// CreateDelivery 依靠唯一请求身份提交一份冻结投递。
func (c *imController) CreateDelivery(ctx context.Context, value models.IMDelivery) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		if dbhelper.IsDuplicateError(err) {
			return status_error.IMConfigurationConflict
		}
		return databaseError("create_im_delivery", value.ID, err)
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("delivery", value.ID))
	return nil
}

// Delivery 读取单份完整投递。
func (c *imController) Delivery(ctx context.Context, id string) (models.IMDelivery, error) {
	var value models.IMDelivery
	if err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error; err != nil {
		return value, imReadError("get_im_delivery", id, err)
	}
	return value, nil
}

// FindDelivery 对账调用者稳定请求身份。
func (c *imController) FindDelivery(ctx context.Context, requestID string) (models.IMDelivery, bool, error) {
	var value models.IMDelivery
	if err := controllerDB(ctx, c.db).Where("request_id = ?", requestID).First(&value).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return value, false, nil
		}
		return value, false, databaseError("find_im_delivery", requestID, err)
	}
	return value, true, nil
}

// Deliveries 分页列出发送事实或后台待发记录。
func (c *imController) Deliveries(ctx context.Context, state constants.IMDeliveryState, limit, offset int) ([]models.IMDelivery, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.IMDelivery{})
	if state != "" {
		db = db.Where("state = ?", state)
	}
	if state == constants.IMDeliveryStatePending {
		// 停用连接不占发送窗口；每个目标只选择最早一项，积压不会阻塞其他聊天。
		db = db.Where("EXISTS (SELECT 1 FROM im_connections c WHERE c.id=im_deliveries.connection_id AND c.enabled=1)").Where("NOT EXISTS (SELECT 1 FROM im_deliveries older WHERE older.binding_id=im_deliveries.binding_id AND older.state IN ? AND older.rowid<im_deliveries.rowid)", []constants.IMDeliveryState{constants.IMDeliveryStatePending, constants.IMDeliveryStateSending})
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, databaseError("count_im_deliveries", "", err)
	}
	result := []models.IMDelivery{}
	if limit > 0 {
		db = db.Limit(limit).Offset(offset)
	}
	order := "created_at DESC, id ASC"
	if state != "" {
		order = "rowid ASC"
	}
	if err := db.Order(order).Find(&result).Error; err != nil {
		return nil, 0, databaseError("list_im_deliveries", "", err)
	}
	return result, count, nil
}

// ClaimDelivery 只允许一次发送者领取仍等待的记录。
func (c *imController) ClaimDelivery(ctx context.Context, id string) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.IMDelivery{}).Where("id = ? AND state = ?", id, constants.IMDeliveryStatePending).Updates(map[string]any{"state": constants.IMDeliveryStateSending, "attempts": gorm.Expr("attempts + 1"), "updated_at": time.Now().UnixMilli()})
	if result.Error != nil {
		return false, databaseError("claim_im_delivery", id, result.Error)
	}
	if result.RowsAffected == 1 {
		publishCommittedChange(ctx, c.db, "im", eventsResource("delivery", id))
	}
	return result.RowsAffected == 1, nil
}

// UpdateDelivery 保存片段收据和最终发送状态。
func (c *imController) UpdateDelivery(ctx context.Context, value models.IMDelivery) error {
	result := controllerDB(ctx, c.db).Model(&models.IMDelivery{}).Where("id = ?", value.ID).Select("state", "receipts", "next_part", "attempts", "error_code", "updated_at").Updates(&value)
	if result.Error != nil {
		return databaseError("update_im_delivery", value.ID, result.Error)
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("delivery", value.ID))
	return nil
}

// RecoverDeliveries 将异常退出时未确认的发送标记为结果不明。
func (c *imController) RecoverDeliveries(ctx context.Context) error {
	var pending []models.IMDelivery
	if err := controllerDB(ctx, c.db).Where("state = ?", constants.IMDeliveryStateSending).Find(&pending).Error; err != nil {
		return databaseError("recover_im_deliveries", "", err)
	}
	for _, delivery := range pending {
		delivery.State = constants.IMDeliveryStateUnknown
		delivery.ErrorCode = "IMDeliveryRetryUnsafe"
		if err := c.UpdateDelivery(ctx, delivery); err != nil {
			return err
		}
	}
	return nil
}

func (c *imController) encrypt(db *gorm.DB, value, identity string) (string, error) {
	key, err := ensureMCPStorageKey(db)
	if err != nil {
		return "", err
	}
	result, err := encryptMCPValue(key, value, []byte(identity))
	if err != nil {
		return "", status_error.IMCredentialError
	}
	return result, nil
}
func (c *imController) decrypt(db *gorm.DB, value, identity string) (string, error) {
	key, err := getMCPStorageKey(db)
	if err != nil {
		return "", status_error.IMCredentialError
	}
	result, err := decryptMCPValue(key, value, []byte(identity))
	if err != nil {
		return "", status_error.IMCredentialError
	}
	return result, nil
}
func imReadError(operation, id string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status_error.IMResourceNotFound
	}
	return databaseError(operation, id, err)
}

// QuoteLink 只在同一授权聊天内定位被引用的输入或已发送消息。
func (c *imController) QuoteLink(ctx context.Context, bindingID, messageID string) (string, error) {
	db := controllerDB(ctx, c.db)
	var input models.IMInbox
	if err := db.Where("binding_id = ? AND external_message_id = ?", bindingID, messageID).Limit(1).Find(&input).Error; err != nil {
		return "", databaseError("quote_im_input", bindingID, err)
	}
	if input.LinkID != "" {
		return input.LinkID, nil
	}
	var delivery models.IMDelivery
	if err := db.Where("binding_id = ? AND EXISTS (SELECT 1 FROM json_each(receipts) WHERE value = ?)", bindingID, messageID).Limit(1).Find(&delivery).Error; err != nil {
		return "", databaseError("quote_im_delivery", bindingID, err)
	}
	return delivery.LinkID, nil
}

// encryptFiles 使用输入身份绑定加密附件能力，数据库快照不携带明文下载参数。
func (c *imController) encryptFiles(ctx context.Context, value *models.IMInbox) error {
	if len(value.Files) == 0 {
		value.FilesCiphertext = ""
		return nil
	}
	raw, err := json.Marshal(value.Files)
	if err != nil {
		return status_error.IMCredentialError
	}
	cipher, err := c.encrypt(controllerDB(ctx, c.db), string(raw), value.ID+"/files")
	if err != nil {
		return err
	}
	value.FilesCiphertext = cipher
	return nil
}

// CheckpointLink 提交安全投影游标和仍待决定的报告，不修改实际对话关系。
func (c *imController) CheckpointLink(ctx context.Context, value models.IMConversationLink) error {
	result := controllerDB(ctx, c.db).Model(&models.IMConversationLink{}).Where("id = ? AND event_cursor <= ?", value.ID, value.EventCursor).Select("event_cursor", "pending_report_ids").Updates(&value)
	if result.Error != nil {
		return databaseError("checkpoint_im_link", value.ID, result.Error)
	}
	publishCommittedChange(ctx, c.db, "im", eventsResource("global", ""))
	return nil
}
