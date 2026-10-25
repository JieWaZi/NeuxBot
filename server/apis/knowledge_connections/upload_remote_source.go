package knowledge_connections

import (
	"encoding/base64"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 远端原件上传输入
type RemoteUploadInput struct {
	// 原件输入
	knowledge.ImportSourceInput
	// 已观察连接版本
	ConnectionRevision int64 `json:"connectionRevision" validate:"min=1"`
	// 已确认上传行为
	UploadMode string `json:"uploadMode" validate:"required,oneof=separate automatic"`
}

// 原生远端上传结果
type RemoteUploadResult struct {
	// 原请求标识
	RequestID string `json:"requestId"`
	// 已接纳
	Accepted bool `json:"accepted"`
	// 原件身份
	SourceID string `json:"sourceId"`
	// 上传行为
	UploadMode string `json:"uploadMode"`
	// 发送事实
	TransferState constants.RemoteTransferState `json:"transferState"`
	// 处理状态
	ProcessingState string `json:"processingState"`
	// 自有端回执
	Receipt *knowledge.Receipt `json:"receipt,omitempty"`
}

// 按远端原生行为上传资料
type UploadRemoteSource struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 上传内容
	Body RemoteUploadInput `in:"body"`
}

func (*UploadRemoteSource) Path() string { return "/:connectionId/libraries/:libraryId/uploads" }
func (o *UploadRemoteSource) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.connection.Backend != "integration" || global.KnowledgeControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	if c.connection.Revision != o.Body.ConnectionRevision || c.connection.Detection.UploadMode != o.Body.UploadMode {
		return nil, status_error.KnowledgeConflict
	}
	if o.Body.UploadMode != "separate" && o.Body.UploadMode != "automatic" {
		return nil, status_error.KnowledgeUnsupported
	}
	raw, err := base64.StdEncoding.DecodeString(o.Body.Bytes)
	if err != nil || len(raw) > 32<<20 {
		return nil, status_error.KnowledgeInvalid
	}
	// 真实业务请求前只读复核服务类型和处理模式，变化时停止发送并要求重新展示提示。
	detected, err := remote.Detect(ctx.Request.Context(), c.options, o.LibraryID)
	if err != nil {
		return nil, remoteError(err)
	}
	if detected.UploadMode != o.Body.UploadMode || detected.ServiceKind != c.connection.Detection.ServiceKind {
		return nil, status_error.KnowledgeConflict
	}
	if o.Body.UploadMode == "separate" && c.custom != nil {
		receipt, err := remoteInvoke(ctx, c, o.Body.RequestID, "source.import", o.Body, func() (*core.Receipt, error) {
			return c.custom.ImportSource(ctx.Request.Context(), core.ImportSourceInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceID: o.Body.SourceID, ExpectedVersion: o.Body.ExpectedVersion, Filename: o.Body.Filename, MIME: o.Body.MIME, Bytes: raw})
		})
		if err != nil {
			return nil, err
		}
		return RemoteUploadResult{RequestID: o.Body.RequestID, Accepted: true, SourceID: receipt.ResourceID, UploadMode: "separate", TransferState: constants.RemoteTransferStateAccepted, ProcessingState: "saved", Receipt: &receipt}, nil
	}
	if detected.ServiceKind != "weknora" || detected.UploadMode != "automatic" || c.official == nil || o.Body.SourceID != "" || o.Body.ExpectedVersion != 0 {
		return nil, status_error.KnowledgeUnsupported
	}
	fresh, err := claimRemoteRequest(ctx, c, o.Body.RequestID, "source.upload.native", o.Body, true)
	if err != nil {
		return nil, err
	}
	if !fresh {
		return nativeUploadStatus(ctx, o.ConnectionID, o.LibraryID, o.Body.RequestID)
	}
	result, err := c.official.UploadNative(ctx.Request.Context(), remote.NativeUploadInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, Filename: o.Body.Filename, MIME: o.Body.MIME, Bytes: raw})
	if err != nil {
		mapped := remoteError(err)
		if errors.Is(mapped, status_error.KnowledgeRemoteOutcomeUnknown) {
			if e := global.KnowledgeControllerManager.MarkRemoteUnknown(ctx, o.Body.RequestID); e != nil {
				return nil, e
			}
		} else {
			var stable interface {
				error
				Code() int64
			}
			if errors.As(mapped, &stable) {
				if e := global.KnowledgeControllerManager.RejectRemoteOperation(ctx, o.Body.RequestID, stable.Code()); e != nil {
					return nil, e
				}
			}
		}
		return nil, mapped
	}
	if err := global.KnowledgeControllerManager.AcceptNativeUpload(ctx, o.Body.RequestID, result.Source.ID); err != nil {
		return nil, err
	}
	return RemoteUploadResult{RequestID: o.Body.RequestID, Accepted: true, SourceID: result.Source.ID, UploadMode: "automatic", TransferState: constants.RemoteTransferStateAccepted, ProcessingState: result.ProcessingState}, nil
}

// 读取原远端上传结果
type GetRemoteUploadStatus struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 原请求标识
	RequestID string `in:"path" name:"requestId" validate:"required"`
}

func (*GetRemoteUploadStatus) Path() string {
	return "/:connectionId/libraries/:libraryId/uploads/:requestId"
}
func (o *GetRemoteUploadStatus) Output(ctx *gin.Context) (interface{}, error) {
	out, err := nativeUploadStatus(ctx, o.ConnectionID, o.LibraryID, o.RequestID)
	if errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		return out, nil
	}
	return out, err
}
func nativeUploadStatus(ctx *gin.Context, connectionID, libraryID, requestID string) (RemoteUploadResult, error) {
	if global.KnowledgeControllerManager == nil {
		return RemoteUploadResult{}, status_error.KnowledgeUnsupported
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, requestID)
	if err != nil {
		return RemoteUploadResult{}, err
	}
	if link.ConnectionID != connectionID || link.KnowledgeBaseID != libraryID {
		return RemoteUploadResult{}, status_error.KnowledgeNotFound
	}
	out := RemoteUploadResult{RequestID: requestID, SourceID: link.ResourceID, UploadMode: link.UploadMode, TransferState: operationDTO(link).TransferState, Accepted: link.TransferState == constants.RemoteTransferStateAccepted, ProcessingState: "unknown"}
	if link.RemoteErrorCode != 0 {
		return out, status_error.StatusError(link.RemoteErrorCode)
	}
	if !link.NativeUpload {
		c, err := recoveryClient(ctx, link)
		if err != nil {
			return out, status_error.KnowledgeRemoteOutcomeUnknown
		}
		receipt, err := reconcileRemote(ctx, c, requestID)
		if err != nil {
			return out, err
		}
		out.Accepted = true
		out.SourceID = receipt.ResourceID
		out.Receipt = &receipt
		out.TransferState = constants.RemoteTransferStateAccepted
		return out, nil
	}
	if !out.Accepted {
		return out, status_error.KnowledgeRemoteOutcomeUnknown
	}
	if original, err := recoveryClient(ctx, link); err == nil && original.official != nil {
		if source, err := original.official.GetSource(ctx.Request.Context(), libraryID, link.ResourceID); err == nil && source.KnowledgeBaseID == libraryID {
			out.ProcessingState = source.ParseStatus
		}
	}
	return out, nil
}
