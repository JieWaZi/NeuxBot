package knowledge_connections

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

// remoteInvoke 只让首次持久接纳的请求发出写入；重启或并发重放均只查询原回执。
func remoteInvoke(ctx *gin.Context, c *remoteClients, requestID, operation string, input any, call func() (*core.Receipt, error)) (knowledge.Receipt, error) {
	if global.KnowledgeControllerManager == nil {
		return knowledge.Receipt{}, status_error.KnowledgeUnsupported
	}
	fresh, err := claimRemoteRequest(ctx, c, requestID, operation, input, false)
	if err != nil {
		return knowledge.Receipt{}, err
	}

	if !fresh {
		return reconcileRemote(ctx, c, requestID)
	}
	receipt, err := call()
	if err != nil {
		mapped := remoteError(err)
		if errors.Is(mapped, status_error.KnowledgeRemoteOutcomeUnknown) {
			if persist := global.KnowledgeControllerManager.MarkRemoteUnknown(ctx, requestID); persist != nil {
				return knowledge.Receipt{}, persist
			}
		} else {
			var stable interface {
				error
				Code() int64
			}
			if errors.As(mapped, &stable) {
				if persist := global.KnowledgeControllerManager.RejectRemoteOperation(ctx, requestID, stable.Code()); persist != nil {
					return knowledge.Receipt{}, persist
				}
			}
		}
		return knowledge.Receipt{}, mapped
	}
	return remoteWriteResult(ctx, c, receipt, nil)
}

// reconcileRemote 从原服务器查询同一请求；回执读取失败不证明原写入被拒绝，不会重新 POST。
func reconcileRemote(ctx *gin.Context, c *remoteClients, requestID string) (knowledge.Receipt, error) {
	if global.KnowledgeControllerManager == nil {
		return knowledge.Receipt{}, status_error.KnowledgeUnsupported
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, requestID)
	if err != nil {
		return knowledge.Receipt{}, err
	}
	if link.ConnectionID != c.connection.ID || (remoteTargetLibrary(c) != "" && link.KnowledgeBaseID != remoteTargetLibrary(c)) {
		return knowledge.Receipt{}, status_error.KnowledgeConflict
	}
	original, err := recoveryClient(ctx, link)
	if err != nil || original.custom == nil {
		return knowledge.Receipt{}, status_error.KnowledgeRemoteOutcomeUnknown
	}
	c = original
	target := c.custom.Resource()

	if link.RemoteErrorCode != 0 {
		return knowledge.Receipt{}, status_error.StatusError(link.RemoteErrorCode)
	}
	if link.Operation == "library.create" {
		// 新库身份可能尚未返回；即使已恢复也不能假定凭据自动获得新库 grant。
		receipt, err := c.custom.GetCreationReceipt(ctx, requestID)
		if err != nil {
			// 当前读取授权、网络或协议失败均不能改写已发送创建的结果。
			return knowledge.Receipt{}, status_error.KnowledgeRemoteOutcomeUnknown
		}
		if link.KnowledgeBaseID != "" && receipt.LibraryID != link.KnowledgeBaseID {
			return knowledge.Receipt{}, status_error.KnowledgeRemoteOutcomeUnknown
		}
		return remoteWriteResult(ctx, c, receipt, nil)
	}
	if link.KnowledgeBaseID == "" {
		return knowledge.Receipt{}, status_error.KnowledgeRemoteOutcomeUnknown
	}
	scoped := c
	if target.KnowledgeBaseID == "" {
		scoped, err = client(ctx, c.connection.ID, link.KnowledgeBaseID)
		if err != nil {
			return knowledge.Receipt{}, status_error.KnowledgeRemoteOutcomeUnknown
		}
	}
	receipt, err := scoped.custom.GetReceipt(ctx, requestID)
	if err != nil {
		// 只有首次写入已持久的 RemoteErrorCode 能证明明确拒绝；读取失败保留原身份。
		return knowledge.Receipt{}, status_error.KnowledgeRemoteOutcomeUnknown
	}
	return remoteWriteResult(ctx, scoped, receipt, nil)
}

func claimRemoteRequest(ctx *gin.Context, c *remoteClients, requestID, operation string, input any, native bool) (bool, error) {
	// 只存摘要与完整目标；原件、正文、凭据和维护要求不进入主库。
	canonical, err := json.Marshal(struct {
		// Ref 固定完整远端目标。
		Ref knowledge.ResourceRef
		// ConnectionRevision 固定连接配置，防止重配后向另一服务器对账。
		ConnectionRevision int64
		// Operation 固定真实远端动作。
		Operation string
		// Input 仅在内存中计算规范化摘要。
		Input any
	}{remoteRef(c, remoteTargetLibrary(c)), c.connection.Revision, operation, input})
	if err != nil {
		return false, status_error.KnowledgeInvalid
	}
	sum := sha256.Sum256(canonical)
	link := models.KnowledgeOperationLink{NativeUpload: native, UploadMode: c.connection.Detection.UploadMode, RequestID: requestID, Operation: operation, Digest: hex.EncodeToString(sum[:]), Backend: c.connection.Backend, ConnectionID: c.connection.ID, KnowledgeBaseID: remoteTargetLibrary(c), ConnectionRevision: c.connection.Revision}
	return global.KnowledgeControllerManager.ClaimRemoteOperation(ctx, link)
}

func remoteTargetLibrary(c *remoteClients) string {
	if c.libraryID != "" {
		return c.libraryID
	}
	if c.custom != nil {
		return c.custom.Resource().KnowledgeBaseID
	}
	return ""
}

// recoveryClient 仅由已持久操作恢复原目标，绕过挂载存在性但不接受任意地址或秘密。
func recoveryClient(ctx *gin.Context, link models.KnowledgeOperationLink) (*remoteClients, error) {
	if global.KnowledgeConnectionControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	if history, ok := global.KnowledgeConnectionControllerManager.(controllers.KnowledgeConnectionHistoryController); ok {
		connection, token, err := history.SnapshotRevision(ctx.Request.Context(), link.ConnectionID, link.ConnectionRevision)
		if err != nil {
			return nil, err
		}
		if connection.Backend != link.Backend {
			return nil, status_error.KnowledgeConflict
		}
		return clientSnapshot(connection, token, link.KnowledgeBaseID)
	}
	connection, token, err := global.KnowledgeConnectionControllerManager.Snapshot(ctx, link.ConnectionID)
	if err != nil {
		return nil, err
	}
	if connection.Revision != link.ConnectionRevision || connection.Backend != link.Backend {
		return nil, status_error.KnowledgeConflict
	}
	return clientSnapshot(connection, token, link.KnowledgeBaseID)
}

// operationDTO 只显示原目标、发送事实和安全结果，不返回历史凭据或请求正文。
func operationDTO(link models.KnowledgeOperationLink) RemoteOperation {
	state := link.TransferState
	if state == "" {
		state = constants.RemoteTransferStateUnknown
		if link.ReceiptConfirmed {
			state = constants.RemoteTransferStateAccepted
		}
		if link.RemoteErrorCode != 0 {
			state = constants.RemoteTransferStateFailed
		}
	}
	return RemoteOperation{Ref: knowledge.ResourceRef{Backend: link.Backend, ConnectionID: link.ConnectionID, KnowledgeBaseID: link.KnowledgeBaseID}, RequestID: link.RequestID, Operation: link.Operation, ReceiptConfirmed: link.ReceiptConfirmed, Rejected: link.RemoteErrorCode != 0, ErrorCode: link.RemoteErrorCode, ResourceID: link.ResourceID, JobID: link.JobID, TransferState: state, UploadMode: link.UploadMode, ConnectionRevision: link.ConnectionRevision, NativeUpload: link.NativeUpload}
}
