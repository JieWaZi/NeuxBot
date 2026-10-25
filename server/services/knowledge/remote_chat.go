package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// RemoteChatCommand 是用户明确选定附件与服务器配置后的独立操作，不启动本机执行。
type RemoteChatCommand struct {
	// 请求标识
	RequestID string
	// 真实聊天
	ChatID string
	// 完整目标
	Ref bridge.ResourceRef
	// 服务器配置
	ProfileID string
	// 明确选择的附件
	Attachments []workspace.AttachmentInput
}

// RemoteChatOperation 返回远端真实回执和任务；未知接纳不伪造Execution。
type RemoteChatOperation struct {
	// 资源引用
	Ref bridge.ResourceRef `json:"ref"`
	// 聊天标识
	ChatID string `json:"chatId"`
	// 操作标识
	RequestID string `json:"requestId"`
	// 接纳状态
	State string `json:"state"`
	// 待确认请求
	PendingRequestID string `json:"pendingRequestId"`
	// 可继续上传
	CanResume bool `json:"canResume"`
	// 原始回执
	Receipt *core.Receipt `json:"receipt,omitempty"`
	// 远端任务
	Job *core.Job `json:"job,omitempty"`
}

// remoteChatScope 固定连接代次与真实项目，不允许期间切换目标。
type remoteChatScope struct {
	// service 复用当前关系与连接权威。
	service *Service
	// provider 固定当前连接配置快照。
	provider *frozenKnowledgeReads
	// chat 保存实际聊天身份与受管目录。
	chat models.Chat
	// ref 固定完整远端资源。
	ref bridge.ResourceRef
	// revision 固定远端配置版本。
	revision int64
}

func (s *Service) remoteChatScope(ctx context.Context, chatID string, ref bridge.ResourceRef) (*remoteChatScope, error) {
	if ref.Backend != "custom" || ref.ConnectionID == "" || ref.ConnectionID == "local" || ref.KnowledgeBaseID == "" {
		return nil, status_error.KnowledgeUnsupported
	}
	summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if summary.Chat.ID != chatID {
		return nil, status_error.KnowledgeForbidden
	}
	provider, err := s.freezeReadProvider(ctx)
	if err != nil {
		return nil, err
	}
	scope := &remoteChatScope{service: s, provider: provider, chat: summary.Chat, ref: ref, revision: provider.Snapshots[ref.ConnectionID].Revision}
	if _, err = scope.client(ctx); err != nil {
		return nil, err
	}
	return scope, nil
}
func (p *remoteChatScope) client(ctx context.Context) (*remote.Custom, error) {
	summary, err := p.service.config.Chats.GetChatSummary(ctx, p.chat.ID)
	if err != nil {
		return nil, err
	}
	if summary.Chat.ProjectID != p.chat.ProjectID || summary.Chat.WakerID != p.chat.WakerID {
		return nil, status_error.KnowledgeForbidden
	}
	if p.chat.ProjectID != "" {
		project, err := p.service.config.Projects.Get(ctx, p.chat.ProjectID)
		if err != nil {
			return nil, err
		}
		if project.WakerID != p.chat.WakerID {
			return nil, status_error.KnowledgeForbidden
		}
	}
	refs, err := p.service.agentReadResources(ctx, p.provider)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(refs, p.ref) {
		return nil, status_error.KnowledgeForbidden
	}
	provider, err := p.provider.remote(ctx, p.ref)
	if err != nil {
		return nil, err
	}
	if provider.Custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return provider.Custom, nil
}

// remoteChatAttachment 只在内存中保存此次明确选择的真实字节。
type remoteChatAttachment struct {
	// name 是用户原件名。
	name string
	// mime 是既有附件校验返回的类型。
	mime string
	// raw 是受管副本读取的实际字节。
	raw []byte
	// hash 固定实际传出的内容摘要。
	hash string
}

func readRemoteChatAttachments(chat models.Chat, inputs []workspace.AttachmentInput) (out []remoteChatAttachment, resultErr error) {
	if len(inputs) == 0 || len(inputs) > 20 {
		return nil, status_error.KnowledgeInvalid
	}
	owner, err := workspace.ReadExecutionWorkspaceOwner(chat.WorkspaceDirectory)
	if err != nil || owner.ChatID != chat.ID || owner.WakerID != chat.WakerID {
		return nil, status_error.KnowledgeForbidden
	}
	prepared, err := workspace.PrepareChatAttachments(chat.WorkspaceDirectory, chat.ID, util.New(), inputs)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := prepared.Rollback(); err != nil && resultErr == nil {
			out = nil
			resultErr = err
		}
	}()
	root, err := os.OpenRoot(chat.WorkspaceDirectory)
	if err != nil {
		return nil, status_error.KnowledgeStorageError
	}
	defer root.Close()
	out = []remoteChatAttachment{}
	total := 0
	for _, ref := range prepared.References {
		file, err := root.Open(ref.Path)
		if err != nil {
			return nil, status_error.KnowledgeInvalid
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 {
			file.Close()
			return nil, status_error.KnowledgeInvalid
		}
		raw, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
		file.Close()
		if err != nil || len(raw) > 32<<20 {
			return nil, status_error.KnowledgeInvalid
		}
		total += len(raw)
		if total > 64<<20 {
			return nil, status_error.KnowledgeInvalid
		}
		sum := sha256.Sum256(raw)
		out = append(out, remoteChatAttachment{name: ref.Name, mime: ref.MediaType, raw: raw, hash: hex.EncodeToString(sum[:])})
	}
	return out, nil
}
func remoteChatStepID(parent, step string) string {
	sum := sha256.Sum256([]byte(parent + "/" + step))
	return "chat-knowledge-" + hex.EncodeToString(sum[:16])
}

// OrganizeRemoteChat 逐项接纳真实HTTP写入；相同子请求一旦发出永远只查询原回执。
func (s *Service) OrganizeRemoteChat(ctx context.Context, in RemoteChatCommand) (*RemoteChatOperation, error) {
	if in.RequestID == "" || len(in.RequestID) > 64 || in.ProfileID == "" {
		return nil, status_error.KnowledgeInvalid
	}
	scope, err := s.remoteChatScope(ctx, in.ChatID, in.Ref)
	if err != nil {
		return nil, err
	}
	client, err := scope.client(ctx)
	if err != nil {
		return nil, err
	}
	capabilities, err := client.Capabilities(ctx)
	if err != nil {
		return nil, bridge.MapRemoteError(err)
	}
	if !capabilities.Operations["rawOnly"] || !capabilities.Operations["organize"] {
		return nil, status_error.KnowledgeForbidden
	}
	if !slices.ContainsFunc(capabilities.Profiles, func(profile remote.ServerProfile) bool { return profile.ID == in.ProfileID }) {
		return nil, status_error.KnowledgeUnsupported
	}
	attachments, err := readRemoteChatAttachments(scope.chat, in.Attachments)
	if err != nil {
		return nil, err
	}
	hashes := []string{}
	for _, attachment := range attachments {
		hashes = append(hashes, attachment.name+"/"+attachment.mime+"/"+attachment.hash)
	}
	canonical, _ := json.Marshal(struct {
		// Chat 固定真实聊天。
		Chat string
		// Project 固定实际项目范围。
		Project string
		// Ref 固定完整目标。
		Ref bridge.ResourceRef
		// Revision 固定连接版本。
		Revision int64
		// Profile 固定服务器配置。
		Profile string
		// Attachments 固定有序真实附件摘要。
		Attachments []string
	}{in.ChatID, scope.chat.ProjectID, in.Ref, scope.revision, in.ProfileID, hashes})
	sum := sha256.Sum256(canonical)
	parent := models.KnowledgeOperationLink{RequestID: in.RequestID, Operation: "chat.remote.organize", Digest: hex.EncodeToString(sum[:]), Backend: in.Ref.Backend, ConnectionID: in.Ref.ConnectionID, KnowledgeBaseID: in.Ref.KnowledgeBaseID, ConnectionRevision: scope.revision, ChatID: in.ChatID}
	if _, err = s.config.References.ClaimRemoteOperation(ctx, parent); err != nil {
		return nil, err
	}
	sourceIDs := []string{}
	for index, attachment := range attachments {
		id := remoteChatStepID(in.RequestID, "source/"+string(rune('a'+index)))
		receipt, err := s.remoteChatStep(ctx, scope, parent, id, "source.import", func(client *remote.Custom) (*core.Receipt, error) {
			return client.ImportSource(ctx, core.ImportSourceInput{RequestID: id, LibraryID: in.Ref.KnowledgeBaseID, Filename: attachment.name, MIME: attachment.mime, Bytes: attachment.raw})
		})
		if errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
			return &RemoteChatOperation{Ref: in.Ref, ChatID: in.ChatID, RequestID: in.RequestID, State: "unknown", PendingRequestID: id}, nil
		}
		if err != nil {
			return nil, err
		}
		sourceIDs = append(sourceIDs, receipt.ResourceID)
	}
	id := remoteChatStepID(in.RequestID, "organize")
	_, err = s.remoteChatStep(ctx, scope, parent, id, "job.accept", func(client *remote.Custom) (*core.Receipt, error) {
		library, err := client.GetLibrary(ctx)
		if err != nil {
			return nil, err
		}
		return client.Organize(ctx, remote.OrganizeRequest{RequestID: id, LibraryID: in.Ref.KnowledgeBaseID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: sourceIDs, ProfileID: in.ProfileID})
	})
	if errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		return &RemoteChatOperation{Ref: in.Ref, ChatID: in.ChatID, RequestID: in.RequestID, State: "unknown", PendingRequestID: id}, nil
	}
	if err != nil {
		return nil, err
	}
	return s.ReadRemoteChatOperation(ctx, in.ChatID, in.RequestID)
}

func (s *Service) remoteChatStep(ctx context.Context, scope *remoteChatScope, parent models.KnowledgeOperationLink, id, operation string, call func(*remote.Custom) (*core.Receipt, error)) (*core.Receipt, error) {
	digest := sha256.Sum256([]byte(parent.Digest + "/" + id + "/" + operation))
	// 客户端引用与同实例服务端的local执行锚点使用不同主键，wire幂等身份保持不变。
	anchorID := remoteChatStepID(parent.RequestID, "host/"+id)
	link := models.KnowledgeOperationLink{RequestID: anchorID, ReceiptRequestID: id, ParentRequestID: parent.RequestID, Operation: operation, Digest: hex.EncodeToString(digest[:]), Backend: parent.Backend, ConnectionID: parent.ConnectionID, ConnectionRevision: parent.ConnectionRevision, KnowledgeBaseID: parent.KnowledgeBaseID, ChatID: parent.ChatID}
	fresh, err := s.config.References.ClaimRemoteOperation(ctx, link)
	if err != nil {
		return nil, err
	}
	if err = s.config.References.LinkReceipt(ctx, parent.RequestID, anchorID); err != nil {
		return nil, err
	}
	if !fresh {
		saved, err := s.config.References.GetOperation(ctx, anchorID)
		if err != nil {
			return nil, err
		}
		if saved.RemoteErrorCode != 0 {
			return nil, status_error.StatusError(saved.RemoteErrorCode)
		}
	}
	s.policy.RLock()
	defer s.policy.RUnlock()
	client, err := scope.client(ctx)
	if err != nil {
		return nil, err
	}
	var receipt *core.Receipt
	if fresh {
		receipt, err = call(client)
	} else {
		receipt, err = client.GetReceipt(ctx, id)
		if errors.Is(bridge.MapRemoteError(err), status_error.KnowledgeNotFound) {
			return nil, status_error.KnowledgeRemoteOutcomeUnknown
		}
	}
	if err != nil {
		mapped := bridge.MapRemoteError(err)
		if !errors.Is(mapped, status_error.KnowledgeRemoteOutcomeUnknown) {
			var stable interface{ Code() int64 }
			if errors.As(mapped, &stable) {
				if persist := s.config.References.RejectRemoteOperation(ctx, anchorID, stable.Code()); persist != nil {
					return nil, persist
				}
			}
		}
		return nil, mapped
	}
	if receipt.Operation != operation || receipt.LibraryID != parent.KnowledgeBaseID {
		return nil, status_error.KnowledgeRemoteOutcomeUnknown
	}
	if err = s.config.References.CompleteOperation(ctx, anchorID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
		return nil, err
	}
	if s.config.Events != nil {
		_ = events.Emit(s.config.Events, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: parent.KnowledgeBaseID, ParentID: parent.ConnectionID}, map[string]any{"ref": scope.ref, "requestId": id})
	}
	return receipt, nil
}

// ReadRemoteChatOperation 只从原连接查询回执和任务；刷新不上传附件或重复整理。
func (s *Service) ReadRemoteChatOperation(ctx context.Context, chatID, requestID string) (*RemoteChatOperation, error) {
	parent, err := s.config.References.GetOperation(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if parent.Operation != "chat.remote.organize" || parent.ChatID != chatID {
		return nil, status_error.KnowledgeForbidden
	}
	ref := bridge.ResourceRef{Backend: parent.Backend, ConnectionID: parent.ConnectionID, KnowledgeBaseID: parent.KnowledgeBaseID}
	scope, err := s.remoteChatScope(ctx, chatID, ref)
	if err != nil {
		return nil, err
	}
	if scope.revision != parent.ConnectionRevision {
		return nil, status_error.KnowledgeForbidden
	}
	// 优先查询固定整理子请求，避免并发上传对账覆盖最后指针后隐藏已接纳任务。
	final, finalErr := s.config.References.GetOperation(ctx, remoteChatStepID(requestID, "host/"+remoteChatStepID(requestID, "organize")))
	if finalErr == nil {
		parent.ReceiptRequestID = final.RequestID
	} else {
		var stable interface{ Code() int64 }
		if !errors.As(finalErr, &stable) || stable.Code() != status_error.KnowledgeNotFound.Code() {
			return nil, finalErr
		}
	}
	out := &RemoteChatOperation{Ref: ref, ChatID: chatID, RequestID: requestID, State: "uploading", PendingRequestID: parent.ReceiptRequestID, CanResume: true}
	if parent.ReceiptRequestID == "" {
		return out, nil
	}
	child, err := s.config.References.GetOperation(ctx, parent.ReceiptRequestID)
	if err != nil {
		return nil, err
	}
	if child.ParentRequestID != parent.RequestID || child.ChatID != chatID || child.ConnectionRevision != parent.ConnectionRevision {
		return nil, status_error.KnowledgeForbidden
	}
	if child.RemoteErrorCode != 0 {
		out.State = "rejected"
		out.CanResume = false
		return out, nil
	}
	s.policy.RLock()
	defer s.policy.RUnlock()
	client, err := scope.client(ctx)
	if err != nil {
		return nil, err
	}
	wireID := child.ReceiptRequestID
	if wireID == "" {
		return nil, status_error.KnowledgeConflict
	}
	out.PendingRequestID = wireID
	receipt, err := client.GetReceipt(ctx, wireID)
	if errors.Is(bridge.MapRemoteError(err), status_error.KnowledgeNotFound) || errors.Is(bridge.MapRemoteError(err), status_error.KnowledgeRemoteOutcomeUnknown) {
		out.State = "unknown"
		out.CanResume = false
		return out, nil
	}
	if err != nil {
		return nil, bridge.MapRemoteError(err)
	}
	if receipt.Operation != child.Operation || receipt.LibraryID != ref.KnowledgeBaseID {
		return nil, status_error.KnowledgeRemoteOutcomeUnknown
	}
	if err = s.config.References.CompleteOperation(ctx, child.RequestID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
		return nil, err
	}
	out.Receipt = receipt
	if child.Operation == "job.accept" {
		out.State = "accepted"
		out.CanResume = false
		out.PendingRequestID = ""
		out.Job, err = client.GetJob(ctx, receipt.JobID)
		if err != nil {
			return nil, bridge.MapRemoteError(err)
		}
		if err = s.config.References.CompleteOperation(ctx, parent.RequestID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListRemoteChatOperations 只从主库恢复本聊天操作引用，不触发远端写入或本机模型。
func (s *Service) ListRemoteChatOperations(ctx context.Context, chatID string, offset, limit int) ([]models.KnowledgeOperationLink, int64, error) {
	summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
	if err != nil {
		return nil, 0, err
	}
	if summary.Chat.ID != chatID {
		return nil, 0, status_error.KnowledgeForbidden
	}
	return s.config.References.ListChatRemoteOperations(ctx, chatID, offset, limit)
}
