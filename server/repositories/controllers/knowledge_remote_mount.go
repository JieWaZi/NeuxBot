package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// KnowledgeRemoteMountController 复用现有连接加密存储并原子保存明确挂载。
type KnowledgeRemoteMountController interface {
	// Add 只在网络验证完成后保存，事务不调用网络。
	Add(context.Context, AddKnowledgeRemoteMountInput) (models.KnowledgeRemoteMount, error)
	// Replay 在网络前恢复已完成添加，重用身份不能替换 URL、Key 或库。
	Replay(context.Context, AddKnowledgeRemoteMountInput) (models.KnowledgeRemoteMount, bool, error)
	// List 只列出明确挂载，不返回服务器其他可见库。
	List(context.Context, string) ([]models.KnowledgeRemoteMount, error)
	// Remove 只删除本机入口，保留连接及原写入恢复锚点。
	Remove(context.Context, string, int64) error
}

// AddKnowledgeRemoteMountInput 的 Detection 来自服务端只读验证。
type AddKnowledgeRemoteMountInput struct {
	// RequestID 绑定本次添加输入。
	RequestID string
	// ConnectionID 是首次请求稳定派生的连接身份。
	ConnectionID string
	// MountID 是首次请求稳定派生的挂载身份。
	MountID string
	// Credential 是仅在内存中使用并进入既有加密库的 Key。
	Credential string
	// Detection 是目标库和标准地址的真实验证结果。
	Detection remote.Detection
}

// knowledgeRemoteMountController 只使用业务主库，不拥有远端内容。
type knowledgeRemoteMountController struct {
	// db 是主库，事务通过 Context 继承。
	db *gorm.DB
	// transactions 复用既有重试和提交事件边界。
	transactions TransactionController
}

func NewKnowledgeRemoteMountController(db *gorm.DB) KnowledgeRemoteMountController {
	return &knowledgeRemoteMountController{db: db, transactions: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}
func mountDigest(in AddKnowledgeRemoteMountInput) string {
	raw, _ := json.Marshal(struct {
		// URL 绑定完整标准地址。
		URL string
		// Credential 仅用于内存摘要，不保存正文。
		Credential string
		// LibraryID 绑定指定库。
		LibraryID string
	}{in.Detection.BaseURL, in.Credential, in.Detection.Library.ID})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (c *knowledgeRemoteMountController) Replay(ctx context.Context, in AddKnowledgeRemoteMountInput) (models.KnowledgeRemoteMount, bool, error) {
	var mutation models.KnowledgeRemoteMountMutation
	r := controllerDB(ctx, c.db).Where("request_id = ?", in.RequestID).Limit(1).Find(&mutation)
	if r.Error != nil {
		return models.KnowledgeRemoteMount{}, false, databaseError("replay_remote_mount", "", r.Error)
	}
	if r.RowsAffected == 0 {
		return models.KnowledgeRemoteMount{}, false, nil
	}
	if mutation.Digest != mountDigest(in) {
		return models.KnowledgeRemoteMount{}, true, status_error.KnowledgeConflict
	}
	var mount models.KnowledgeRemoteMount
	r = controllerDB(ctx, c.db).Where("id = ?", mutation.MountID).Limit(1).Find(&mount)
	if r.Error != nil {
		return mount, true, databaseError("read_remote_mount", "", r.Error)
	}
	if r.RowsAffected == 0 {
		return mount, true, status_error.KnowledgeConflict
	}
	return mount, true, nil
}
func (c *knowledgeRemoteMountController) Add(ctx context.Context, in AddKnowledgeRemoteMountInput) (out models.KnowledgeRemoteMount, err error) {
	if in.RequestID == "" || in.ConnectionID == "" || in.MountID == "" || in.Credential == "" || in.Detection.Library.ID == "" || in.Detection.Library.Name == "" {
		return out, status_error.KnowledgeInvalid
	}
	err = c.transactions.Transaction(ctx, func(tx context.Context) error {
		old, exists, e := c.Replay(tx, in)
		if e != nil {
			return e
		}
		if exists {
			out = old
			return nil
		}
		_, e = NewKnowledgeConnectionController(c.db).Save(tx, SaveKnowledgeConnectionInput{RequestID: in.RequestID, Credential: in.Credential, Connection: models.KnowledgeConnection{ID: in.ConnectionID, Backend: "integration", AuthMode: "api_key", Name: in.Detection.Library.Name, BaseURL: in.Detection.BaseURL, Enabled: true, Detection: in.Detection}})
		if e != nil {
			return e
		}
		out = models.KnowledgeRemoteMount{ID: in.MountID, ConnectionID: in.ConnectionID, KnowledgeBaseID: in.Detection.Library.ID, Revision: 1}
		if e = controllerDB(tx, c.db).Create(&out).Error; e != nil {
			return databaseError("save_remote_mount", in.MountID, e)
		}
		return databaseErrorOrNil(controllerDB(tx, c.db).Create(&models.KnowledgeRemoteMountMutation{RequestID: in.RequestID, Digest: mountDigest(in), MountID: out.ID}).Error)
	})
	return out, err
}
func databaseErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return databaseError("save_remote_mount_receipt", "", err)
}
func (c *knowledgeRemoteMountController) List(ctx context.Context, connectionID string) ([]models.KnowledgeRemoteMount, error) {
	rows := []models.KnowledgeRemoteMount{}
	query := controllerDB(ctx, c.db).Order("id").Limit(1001)
	if connectionID != "" {
		query = query.Where("connection_id = ?", connectionID)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, databaseError("list_remote_mounts", connectionID, err)
	}
	if len(rows) > 1000 {
		return nil, status_error.KnowledgeInvalid
	}
	return rows, nil
}
func (c *knowledgeRemoteMountController) Remove(ctx context.Context, id string, version int64) error {
	if id == "" || version < 1 {
		return status_error.KnowledgeInvalid
	}
	r := controllerDB(ctx, c.db).Where("id = ? AND revision = ?", id, version).Delete(&models.KnowledgeRemoteMount{})
	if r.Error != nil {
		return databaseError("remove_remote_mount", id, r.Error)
	}
	if r.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := controllerDB(ctx, c.db).Model(&models.KnowledgeRemoteMount{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return databaseError("read_remote_mount", id, err)
	}
	if count > 0 {
		return status_error.KnowledgeConflict
	}
	return nil
}
