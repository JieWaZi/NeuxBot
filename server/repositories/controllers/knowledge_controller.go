package controllers

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"time"
)

// KnowledgeController 仅拥有本机接入引用与跨库操作锚点。
type KnowledgeController interface {
	// LibraryEmbeddingProfile 返回库独立嵌入配置，不含凭据。
	LibraryEmbeddingProfile(context.Context, string) (models.LibraryEmbeddingProfile, error)
	// LibraryEmbeddingSnapshot 冻结此库配置和对应密钥。
	LibraryEmbeddingSnapshot(context.Context, string) (models.LibraryEmbeddingProfile, string, error)
	// PrepareLibraryEmbedding 固定跨核心保存的意图。
	PrepareLibraryEmbedding(context.Context, PrepareLibraryEmbeddingInput) (models.LibraryEmbeddingMutation, error)
	// CommitLibraryEmbedding 启用已获核心回执的独立配置。
	CommitLibraryEmbedding(context.Context, string, int64) (models.LibraryEmbeddingProfile, error)

	// ReplayLibraryExecutionDefaults 恢复精确已提交请求，不受之后Runtime设置变化影响。
	ReplayLibraryExecutionDefaults(context.Context, string, int64, models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, bool, error)
	// LibraryExecutionDefaults 返回唯一库执行偏好，未保存时version为零。
	LibraryExecutionDefaults(context.Context, string) (models.LibraryExecutionDefaults, error)
	// UpdateLibraryExecutionDefaults 原子保存偏好和幂等回执。
	UpdateLibraryExecutionDefaults(context.Context, string, int64, models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error)
	DeleteLibraryReferences(context.Context, string, string, string) error

	RecordLibrarySummary(context.Context, models.KnowledgeLibrarySummary) error
	LibrarySummaries(context.Context, string, string, int64, []string) ([]models.KnowledgeLibrarySummary, error)
	// ListChatRemoteOperations 只返回实际聊天的远端整理引用，不复制任务或原件。
	ListChatRemoteOperations(context.Context, string, int, int) ([]models.KnowledgeOperationLink, int64, error)
	ClaimRemoteOperation(context.Context, models.KnowledgeOperationLink) (bool, error)
	// MarkRemoteUnknown 保留原请求身份，不通过重传恢复。
	MarkRemoteUnknown(context.Context, string) error
	// AcceptNativeUpload 只保存原生接纳和资料身份，不生成核心回执。
	AcceptNativeUpload(context.Context, string, string) error
	// RecoverRemoteSending 在重启时将未确认发送转为未知。
	RecoverRemoteSending(context.Context) error
	ListRemoteOperations(context.Context, string, string, string, int, int) ([]models.KnowledgeOperationLink, int64, error)
	RejectRemoteOperation(context.Context, string, int64) error
	BeginOperation(context.Context, models.KnowledgeOperationLink) error
	CompleteOperation(context.Context, string, string, string, string) error
	LinkReceipt(context.Context, string, string) error
	SetHostDiagnostic(context.Context, string, string, string, string) error
	GetOperation(context.Context, string) (models.KnowledgeOperationLink, error)
	ListPendingOperations(context.Context) ([]models.KnowledgeOperationLink, error)
	// ListJobRetryOperations 只枚举原任务的授权候选，是否接纳由核心回执决定。
	ListJobRetryOperations(context.Context, string) ([]models.KnowledgeOperationLink, error)
	// FreezeTaskConfiguration 固定知识作业首次接纳的模型选择。
	FreezeTaskConfiguration(context.Context, models.KnowledgeTaskConfiguration) (models.KnowledgeTaskConfiguration, error)
	// GetTaskConfiguration 读取知识作业的独立模型配置。
	GetTaskConfiguration(context.Context, string) (models.KnowledgeTaskConfiguration, error)
}

// knowledgeController 使用主库保存引用，不读写知识核心数据库。
type knowledgeController struct {
	// db 是默认主库连接，事务通过 Context 继承。
	db *gorm.DB
	// transactions 使首次发送登记与当前连接及挂载校验保持原子。
	transactions TransactionController
}

// NewKnowledgeController 创建仅保存本机知识引用的Repository。
func NewKnowledgeController(db *gorm.DB) KnowledgeController {
	return &knowledgeController{db: db, transactions: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

// BeginOperation 先持久化稳定请求身份并拒绝同键异内容。
func (c *knowledgeController) BeginOperation(ctx context.Context, in models.KnowledgeOperationLink) error {
	if in.RequestID == "" || in.Digest == "" || in.Operation == "" {
		return status_error.KnowledgeInvalid
	}
	if in.ResourceID != "" {
		in.ReceiptConfirmed = true
	}
	if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&in).Error; err != nil {
		return databaseError("prepare_knowledge_operation", in.RequestID, err)
	}
	old, err := c.GetOperation(ctx, in.RequestID)
	if err != nil {
		return err
	}
	if old.Digest != in.Digest || old.Operation != in.Operation || old.Backend != in.Backend || old.ConnectionID != in.ConnectionID {
		return status_error.KnowledgeConflict
	}
	return nil
}

// CompleteOperation 用实际核心回执补齐跨库资源关联。
func (c *knowledgeController) CompleteOperation(ctx context.Context, id, kb, resource, job string) error {
	err := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("request_id = ?", id).Updates(map[string]any{"knowledge_base_id": kb, "resource_id": resource, "job_id": job, "receipt_confirmed": true, "transfer_state": constants.RemoteTransferStateAccepted}).Error
	if err != nil {
		return databaseError("complete_knowledge_operation", id, err)
	}
	return nil
}

// ListJobRetryOperations 返回重试授权候选，不将宿主字段当作接纳事实。
func (c *knowledgeController) ListJobRetryOperations(ctx context.Context, job string) ([]models.KnowledgeOperationLink, error) {
	var rows []models.KnowledgeOperationLink
	err := controllerDB(ctx, c.db).Where("operation = ? AND expected_resource_id = ?", constants.KnowledgeRetryAuthorityOperation, job).Order("target_attempt ASC").Find(&rows).Error
	return rows, knowledgeReferenceError("list_knowledge_retry_authority", job, err)
}

// GetOperation 读取稳定请求的本机锚点。
func (c *knowledgeController) GetOperation(ctx context.Context, id string) (models.KnowledgeOperationLink, error) {
	var out models.KnowledgeOperationLink
	err := controllerDB(ctx, c.db).Where("request_id = ?", id).First(&out).Error
	return out, knowledgeReferenceError("get_knowledge_operation", id, err)
}

// ListPendingOperations 枚举尚未用权威回执确认的跨库引用。
func (c *knowledgeController) ListPendingOperations(ctx context.Context) ([]models.KnowledgeOperationLink, error) {
	out := []models.KnowledgeOperationLink{}
	err := controllerDB(ctx, c.db).Where("receipt_confirmed = ? AND backend = ? AND connection_id = ?", false, "local", "local").Find(&out).Error
	if err != nil {
		return nil, databaseError("list_knowledge_operations", "", err)
	}
	return out, nil
}

func knowledgeReferenceError(op, id string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status_error.KnowledgeNotFound
	}
	return databaseError(op, id, err)
}

// LinkReceipt 仅连接入口与实际核心写回执，不保存正文或候选。
func (c *knowledgeController) LinkReceipt(ctx context.Context, request, receipt string) error {
	err := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("request_id = ?", request).Update("receipt_request_id", receipt).Error
	if err != nil {
		return databaseError("link_knowledge_receipt", request, err)
	}
	return nil
}

// SetHostDiagnostic 只更新原操作的宿主诊断，不改知识核心状态或持久回执。
func (c *knowledgeController) SetHostDiagnostic(ctx context.Context, request, state, code, diagnostic string) error {
	result := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("request_id = ? AND operation = ?", request, "job.accept").Updates(map[string]any{"host_state": state, "host_error_code": code, "host_diagnostic": diagnostic})
	if result.Error != nil {
		return databaseError("diagnose_knowledge_host", request, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.KnowledgeNotFound
	}
	return nil
}

// ClaimRemoteOperation 在发送HTTP前持久化一次尝试；已存在的未知请求只能查询原回执。
func (c *knowledgeController) ClaimRemoteOperation(ctx context.Context, in models.KnowledgeOperationLink) (bool, error) {
	if in.RequestID == "" || in.Operation == "" || in.Digest == "" || (in.Backend != "custom" && in.Backend != "integration" && in.Backend != "weknora") || in.ConnectionID == "" || in.ConnectionID == "local" || in.ConnectionRevision < 1 {
		return false, status_error.KnowledgeInvalid
	}
	if in.CreatedAt == 0 {
		in.CreatedAt = time.Now().UnixMilli()
	}
	in.TransferState = constants.RemoteTransferStateSending
	in.ReceiptConfirmed = false
	in.ResourceID = ""
	in.JobID = ""
	in.RemoteErrorCode = 0

	fresh := false
	err := c.transactions.Transaction(ctx, func(txctx context.Context) error {
		// 重放只核对首次发送事实，后续禁用或卸载不能改写原请求身份。
		fresh = false
		db := controllerDB(txctx, c.db)
		var old models.KnowledgeOperationLink
		existing := db.Where("request_id = ?", in.RequestID).Limit(1).Find(&old)
		if existing.Error != nil {
			return databaseError("read_remote_knowledge_operation", in.RequestID, existing.Error)
		}
		if existing.RowsAffected != 0 {
			sameLibrary := old.KnowledgeBaseID == in.KnowledgeBaseID || (in.Operation == "library.create" && in.KnowledgeBaseID == "")
			if old.Digest != in.Digest || old.Operation != in.Operation || old.Backend != in.Backend || old.ConnectionID != in.ConnectionID || old.ConnectionRevision != in.ConnectionRevision || !sameLibrary {
				return status_error.KnowledgeConflict
			}
			return nil
		}
		if in.Backend == "integration" {
			var connection models.KnowledgeConnection
			current := db.Where("id = ?", in.ConnectionID).Limit(1).Find(&connection)
			if current.Error != nil {
				return databaseError("check_remote_operation_connection", in.ConnectionID, current.Error)
			}
			if current.RowsAffected == 0 {
				return status_error.KnowledgeNotFound
			}
			if !connection.Enabled {
				return status_error.KnowledgeForbidden
			}
			if connection.Backend != in.Backend || connection.Revision != in.ConnectionRevision {
				return status_error.KnowledgeConflict
			}
			if in.KnowledgeBaseID != "" {
				var count int64
				if err := db.Model(&models.KnowledgeRemoteMount{}).Where("connection_id = ? AND knowledge_base_id = ?", in.ConnectionID, in.KnowledgeBaseID).Count(&count).Error; err != nil {
					return databaseError("check_remote_operation_mount", in.ConnectionID, err)
				}
				if count != 1 {
					return status_error.KnowledgeNotFound
				}
			}
		}
		if err := db.Create(&in).Error; err != nil {
			return databaseError("claim_remote_knowledge_operation", in.RequestID, err)
		}
		fresh = true
		return nil
	})
	return fresh && err == nil, err
}

// ListRemoteOperations 只分页读取当前完整ref尚未确认的操作引用，不读取来源正文。
func (c *knowledgeController) ListRemoteOperations(ctx context.Context, backend, connection, library string, offset, limit int) ([]models.KnowledgeOperationLink, int64, error) {
	if offset < 0 || limit < 0 || limit > 100 {
		return nil, 0, status_error.KnowledgeInvalid
	}
	if limit == 0 {
		limit = 50
	}
	query := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("backend IN ? AND receipt_confirmed = ? AND remote_error_code = ?", []string{"custom", "integration", "weknora"}, false, 0).Where("operation <> ? AND parent_request_id = ?", "chat.remote.organize", "").Where("native_upload = ? OR transfer_state <> ?", false, constants.RemoteTransferStateAccepted)
	if backend != "" {
		query = query.Where("backend = ?", backend)
	}
	if connection != "" {
		query = query.Where("connection_id = ?", connection)
	}
	if library != "" {
		query = query.Where("knowledge_base_id = ?", library)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_remote_knowledge_operations", connection, err)
	}
	rows := []models.KnowledgeOperationLink{}
	if err := query.Order("request_id ASC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, databaseError("list_remote_knowledge_operations", connection, err)
	}
	return rows, total, nil
}

// RejectRemoteOperation 只记录明确拒绝；网络未知不能被标记为没有提交。
func (c *knowledgeController) RejectRemoteOperation(ctx context.Context, id string, code int64) error {
	if code == 0 {
		return status_error.KnowledgeInvalid
	}
	result := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("request_id = ? AND backend IN ? AND receipt_confirmed = ?", id, []string{"custom", "integration", "weknora"}, false).Updates(map[string]any{"remote_error_code": code, "transfer_state": constants.RemoteTransferStateFailed})
	if result.Error != nil {
		return databaseError("reject_remote_knowledge_operation", id, result.Error)
	}
	return nil
}

// ListChatRemoteOperations 使用单表分页恢复聊天入口；清理浮层不会丢失未知写入身份。
func (c *knowledgeController) ListChatRemoteOperations(ctx context.Context, chatID string, offset, limit int) ([]models.KnowledgeOperationLink, int64, error) {
	if chatID == "" || offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, status_error.KnowledgeInvalid
	}
	query := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("chat_id = ? AND operation = ?", chatID, "chat.remote.organize")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_chat_knowledge_operations", chatID, err)
	}
	values := []models.KnowledgeOperationLink{}
	if err := query.Order("created_at DESC, request_id DESC").Offset(offset).Limit(limit).Find(&values).Error; err != nil {
		return nil, 0, databaseError("list_chat_knowledge_operations", chatID, err)
	}
	return values, total, nil
}

// FreezeTaskConfiguration 以稳定请求身份原子固定首份配置，不采用后来变化的默认模型。
func (c *knowledgeController) FreezeTaskConfiguration(ctx context.Context, in models.KnowledgeTaskConfiguration) (models.KnowledgeTaskConfiguration, error) {
	if in.ID == "" || in.KnowledgeBaseID == "" {
		return in, status_error.KnowledgeInvalid
	}
	if in.RuntimeID == "" {
		if _, err := c.GetTaskConfiguration(ctx, in.ID); err != nil {
			if errors.Is(err, status_error.KnowledgeNotFound) {
				return in, status_error.KnowledgeInvalid
			}
			return in, err
		}
	}
	if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&in).Error; err != nil {
		return in, databaseError("freeze_knowledge_task_configuration", in.ID, err)
	}
	stored, err := c.GetTaskConfiguration(ctx, in.ID)
	if err != nil {
		return stored, err
	}
	if stored.Backend != in.Backend || stored.ConnectionID != in.ConnectionID || stored.KnowledgeBaseID != in.KnowledgeBaseID || stored.Message != in.Message || stored.RemoteSubject != in.RemoteSubject || stored.RemoteProfileID != in.RemoteProfileID || string(stored.RemoteAuthorization) != string(in.RemoteAuthorization) || !reflect.DeepEqual(stored.PublicationAuthorizations, in.PublicationAuthorizations) {
		return stored, status_error.KnowledgeConflict
	}
	return stored, nil
}

// GetTaskConfiguration 从宿主独立配置表恢复知识作业选择，不读取普通聊天。
func (c *knowledgeController) GetTaskConfiguration(ctx context.Context, id string) (models.KnowledgeTaskConfiguration, error) {
	var out models.KnowledgeTaskConfiguration
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&out).Error
	return out, knowledgeReferenceError("get_knowledge_task_configuration", id, err)
}

// MarkRemoteUnknown 不覆盖已经确认的结果，所有后续恢复继续使用原请求和原目标。
func (c *knowledgeController) MarkRemoteUnknown(ctx context.Context, id string) error {
	err := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("request_id = ? AND receipt_confirmed = ? AND remote_error_code = ? AND transfer_state <> ?", id, false, 0, constants.RemoteTransferStateAccepted).Update("transfer_state", constants.RemoteTransferStateUnknown).Error
	if err != nil {
		return databaseError("mark_remote_knowledge_unknown", id, err)
	}
	return nil
}
func (c *knowledgeController) AcceptNativeUpload(ctx context.Context, id, sourceID string) error {
	if id == "" || sourceID == "" {
		return status_error.KnowledgeInvalid
	}
	result := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("request_id = ? AND native_upload = ? AND remote_error_code = ?", id, true, 0).Updates(map[string]any{"resource_id": sourceID, "transfer_state": constants.RemoteTransferStateAccepted})
	if result.Error != nil {
		return databaseError("accept_native_upload", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.KnowledgeNotFound
	}
	return nil
}
func (c *knowledgeController) RecoverRemoteSending(ctx context.Context) error {
	err := controllerDB(ctx, c.db).Model(&models.KnowledgeOperationLink{}).Where("backend IN ? AND transfer_state = ? AND receipt_confirmed = ?", []string{"custom", "integration", "weknora"}, constants.RemoteTransferStateSending, false).Update("transfer_state", constants.RemoteTransferStateUnknown).Error
	if err != nil {
		return databaseError("recover_remote_knowledge_sending", "", err)
	}
	return nil
}
