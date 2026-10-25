package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

// ForkInput 保存用户明确选择的会话、回合及幂等请求身份。
type ForkInput struct {
	// ChatID 是普通伙伴来源会话。
	ChatID string
	// ExecutionID 是来源中结束的回合。
	ExecutionID string
	// ClientRequestID 在同一次逻辑操作的网络重试间保持不变。
	ClientRequestID string
}

// NativeForkInput 只向内部 Runtime 端口提供冻结的原生执行事实。
type NativeForkInput struct {
	// ReceiptDirectory 保存适配器对账记录，不属于前端可访问的工作区。
	ReceiptDirectory string
	// ReconcileOnly 要求适配器只对账上一次原生结果，不能重新创建。
	ReconcileOnly bool
	// Invocation 是来源回合实际使用的运行时、模型、权限与原生位置。
	Invocation eventlog.InvocationFinishedData
	// Workspace 是本次子会话的独立受管目录。
	Workspace string
}

// NativeFork 在明确越过原生副作用边界前验证配置，再创建独立 Session。
type NativeFork struct {
	// Preflight 只做无原生副作用的验证；失败时同一请求可以安全重试。
	Preflight func(context.Context, NativeForkInput) error
	// Execute 创建或对账原生 Session；不能发送历史 Prompt。
	Execute func(context.Context, NativeForkInput) (string, error)
}

// forkPayload 固化发布所需的历史，避免重试时读取已经变化的父会话。
type forkPayload struct {
	// Created 是子 Chat 的身份和冻结项目范围。
	Created eventlog.ChatCreatedData `json:"created"`
	// Messages 是重新分配展示身份后的继承历史。
	Messages []messageview.InheritedMessage `json:"messages"`
	// Invocation 是分叉边界的原生执行事实。
	Invocation eventlog.InvocationFinishedData `json:"invocation"`
	// LastExecutionID 是子 Session 对应的最后继承消息身份，仅用于续接游标。
	LastExecutionID string `json:"lastExecutionId"`
}

// ForkChat 在共享操作锁下准备历史、调用原生分叉并发布可重建的完整事实。
func (m *service) ForkChat(ctx context.Context, input ForkInput, fork NativeFork) (string, error) {
	input.ChatID = strings.TrimSpace(input.ChatID)
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.ChatID == "" || input.ExecutionID == "" || input.ClientRequestID == "" || len(input.ClientRequestID) > 128 || fork.Execute == nil {
		return "", status_error.BadRequest
	}
	directory := m.storage.ForkDirectory(input.ChatID, input.ClientRequestID)
	observed, observedExists, err := chatstorage.ReadForkOperation(directory)
	if err != nil {
		return "", err
	}
	if observedExists && (observed.SourceChatID != input.ChatID || observed.ExecutionID != input.ExecutionID) {
		return "", status_error.CreateRequestConflict
	}
	// 已终结的请求不再持有历史负载；只需来源锁即可返回幂等结果。
	if observedExists && (observed.Published || observed.Cancelled) {
		release, lockErr := chatstorage.AcquireOperation(m.storage.Root(), input.ChatID)
		if lockErr != nil {
			return "", lockErr
		}
		defer release()
		if observed.Published {
			return observed.ChatID, nil
		}
		return "", status_error.ChatForkUnavailable
	}
	var wakerID string
	if observedExists {
		var prepared forkPayload
		if json.Unmarshal(observed.Payload, &prepared) != nil {
			return "", status_error.ChatLogCorrupted
		}
		wakerID = prepared.Created.WakerID
	} else {
		_, state, readErr := m.loadChatState(input.ChatID)
		if readErr != nil {
			return "", readErr
		}
		wakerID = state.chat.WakerID
	}
	if wakerID == "" {
		return "", status_error.ChatLogCorrupted
	}
	release, err := chatstorage.AcquireOperation(m.storage.Root(), input.ChatID, "waker:"+wakerID)
	if err != nil {
		return "", err
	}
	defer release()
	operation, exists, err := chatstorage.ReadForkOperation(directory)
	if err != nil {
		return "", err
	}
	if exists && (operation.SourceChatID != input.ChatID || operation.ExecutionID != input.ExecutionID) {
		return "", status_error.CreateRequestConflict
	}
	if !exists {
		operation, err = m.prepareFork(input)
		if err != nil {
			return "", err
		}
		if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
			return "", err
		}
	}
	if operation.Published {
		return operation.ChatID, nil
	}
	if operation.Cancelled {
		return "", status_error.ChatForkUnavailable
	}
	var payload forkPayload
	if json.Unmarshal(operation.Payload, &payload) != nil {
		return "", status_error.ChatLogCorrupted
	}
	if payload.Created.WakerID != wakerID {
		return "", status_error.ChatLogCorrupted
	}
	if !operation.NativeStarted {
		records, state, readErr := m.loadChatState(input.ChatID)
		if readErr != nil {
			return "", readErr
		}
		points, pointErr := forkPoints(records, state)
		if pointErr != nil {
			return "", pointErr
		}
		valid := false
		for _, point := range points {
			if point.target.ExecutionID == input.ExecutionID && point.target.Available && point.invocation.SessionOutputID == payload.Invocation.SessionOutputID && point.invocation.ForkPosition == payload.Invocation.ForkPosition {
				valid = true
				break
			}
		}
		if !valid {
			return "", status_error.ChatForkUnavailable
		}
	}
	if !operation.Prepared {
		if err = m.prepareForkWorkspace(operation.ChatID, payload); err != nil {
			return "", err
		}
		operation.Prepared = true
		if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
			return "", err
		}
	}
	if operation.SessionID == "" {
		reconcileOnly := operation.NativeStarted
		if !reconcileOnly && fork.Preflight != nil {
			if err = fork.Preflight(ctx, NativeForkInput{
				ReceiptDirectory: directory,
				Invocation:       payload.Invocation,
				Workspace:        payload.Created.WorkspaceDirectory,
			}); err != nil {
				return "", err
			}
		}
		operation.NativeStarted = true
		if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
			return "", err
		}
		sessionID, forkErr := fork.Execute(ctx, NativeForkInput{ReceiptDirectory: directory, Invocation: payload.Invocation, Workspace: payload.Created.WorkspaceDirectory, ReconcileOnly: reconcileOnly})
		if forkErr != nil {
			return "", forkErr
		}
		if sessionID == "" || sessionID == payload.Invocation.SessionOutputID {
			return "", status_error.ChatForkIndeterminate
		}
		operation.SessionID = sessionID
		if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
			return "", err
		}
	}
	releaseChild, err := chatstorage.AcquireOperation(m.storage.Root(), operation.ChatID)
	if err != nil {
		return "", err
	}
	defer releaseChild()
	if err = m.publishFork(ctx, directory, operation, payload); err != nil {
		return "", err
	}
	operation.Published = true
	operation.Payload = nil
	if err = chatstorage.WriteForkOperation(directory, operation); err != nil {
		return "", err
	}
	return operation.ChatID, nil
}

func (m *service) prepareFork(input ForkInput) (chatstorage.ForkOperation, error) {
	records, state, err := m.loadChatState(input.ChatID)
	if err != nil {
		return chatstorage.ForkOperation{}, err
	}
	points, err := forkPoints(records, state)
	if err != nil {
		return chatstorage.ForkOperation{}, err
	}
	var selected *forkPoint
	for i := range points {
		if points[i].target.ExecutionID == input.ExecutionID {
			selected = &points[i]
			break
		}
	}
	if selected == nil || !selected.target.Available {
		return chatstorage.ForkOperation{}, status_error.ChatForkUnavailable
	}
	messages, err := messageview.ReplayMessages(records[:selected.end])
	if err != nil {
		return chatstorage.ForkOperation{}, err
	}
	chatID := util.New()
	target, err := m.storage.ChatWorkspace(chatID)
	if err != nil {
		return chatstorage.ForkOperation{}, err
	}
	payload := forkPayload{
		Created: eventlog.ChatCreatedData{
			WakerID:                      state.chat.WakerID,
			ProjectID:                    state.chat.ProjectID,
			ProjectName:                  state.chat.ProjectName,
			WorkspaceDirectory:           target,
			AdditionalWorkingDirectories: state.chat.AdditionalWorkingDirectories,
			Title:                        state.chat.Title + " · 分支",
			ProjectSources:               state.projectSources,
			ForkSourceChatID:             input.ChatID,
			ForkSourceExecutionID:        input.ExecutionID,
			ForkConfiguration:            &selected.configuration,
		},
		Invocation: selected.invocation,
	}
	for _, message := range messages {
		sourceMessageID := message.ID
		message.ID = util.New()
		message.SourceChatID = input.ChatID
		message.Inherited = true
		message.RunMetrics = nil
		if message.ExecutionID != "" {
			message.ExecutionID = message.ID
			payload.LastExecutionID = message.ID
		}
		for i := range message.DisplayParts {
			message.DisplayParts[i].ID = util.New()
		}
		payload.Messages = append(payload.Messages, messageview.InheritedMessage{Message: message, SourceMessageID: sourceMessageID, Diffs: message.FileChangeDiffs})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return chatstorage.ForkOperation{}, status_error.ChatLogError
	}
	return chatstorage.ForkOperation{ChatID: chatID, SourceChatID: input.ChatID, ExecutionID: input.ExecutionID, Payload: data}, nil
}

// prepareForkWorkspace 在持久意图存在后复制文件；失败重试沿用相同目标身份。
func (m *service) prepareForkWorkspace(chatID string, payload forkPayload) error {
	links := make([]workspace.ProjectLink, 0, len(payload.Created.ProjectSources))
	for _, source := range payload.Created.ProjectSources {
		links = append(links, workspace.ProjectLink{EntryName: source.EntryName, SourcePath: source.SourcePath})
	}
	if err := workspace.EnsureExecutionWorkspace(payload.Created.WorkspaceDirectory, chatID, payload.Created.WakerID, links); err != nil {
		return err
	}
	for _, message := range payload.Messages {
		for _, attachment := range message.Attachments {
			if attachment.Managed {
				if err := chatstorage.CopyForkAttachment(payload.Invocation.WorkingDirectory, payload.Created.WorkspaceDirectory, attachment.Path); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (m *service) publishFork(ctx context.Context, directory string, operation chatstorage.ForkOperation, payload forkPayload) error {
	stagingRoot := filepath.Join(directory, "staging")
	staging, err := chatstorage.NewLayout(stagingRoot)
	if err != nil {
		return err
	}
	targetLog, err := m.storage.ChatLogDirectory(operation.ChatID)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(targetLog); os.IsNotExist(statErr) {
		if err = os.RemoveAll(stagingRoot); err != nil {
			return status_error.ChatLogError
		}
		store, storeErr := eventlog.NewStore(stagingRoot)
		if storeErr != nil {
			return storeErr
		}
		facts := []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(payload.Created)}}
		for _, message := range payload.Messages {
			if message.ID == payload.LastExecutionID {
				invocation := payload.Invocation
				invocation.SessionInputID = ""
				invocation.SessionOutputID = operation.SessionID
				invocation.WorkingDirectory = payload.Created.WorkspaceDirectory
				message.ForkInvocation = &invocation
				message.ForkConfiguration = payload.Created.ForkConfiguration
			}
			facts = append(facts, eventlog.AppendInput{Type: constants.ChatEventTypeMessageInherited, MessageID: message.ID, Data: marshalData(message)})
		}
		facts = append(facts, eventlog.AppendInput{Type: constants.ChatEventTypeNativeSessionUpdated, Data: marshalData(eventlog.RuntimeSessionData{RuntimeID: payload.Invocation.RuntimeID, SessionID: operation.SessionID, LastExecutionID: payload.LastExecutionID})})
		path, pathErr := store.LogPath(operation.ChatID)
		if pathErr != nil {
			return pathErr
		}
		if _, statErr = os.Stat(path); os.IsNotExist(statErr) {
			if _, err = store.Create(operation.ChatID, facts); err != nil {
				return err
			}
		}
		if err = m.storage.PublishForkLog(staging, operation.ChatID); err != nil {
			return err
		}
	} else if statErr != nil {
		return status_error.ChatLogError
	}
	_, state, err := m.loadChatState(operation.ChatID)
	if err != nil {
		return err
	}
	return m.queryIndexes.replaceCurrentProjection(ctx, currentProjection{chat: state.chat, runtimeSessions: state.runtimeSessions})
}

// recoverForks 完成已经取得原生身份的持久操作；结果未知的原生调用保持待对账。
func (m *service) recoverForks(ctx context.Context) error {
	return m.visitForkOperations(func(directory string, operation chatstorage.ForkOperation) error {
		if operation.Published {
			return nil
		}
		if operation.Cancelled {
			return m.removeUnpublishedForkFiles(directory, operation.ChatID)
		}
		childLog, err := m.forkLogExists(operation.ChatID)
		if err != nil {
			return err
		}
		sourceLog, err := m.forkLogExists(operation.SourceChatID)
		if err != nil {
			return err
		}
		if !childLog && !sourceLog {
			return m.cancelUnpublishedFork(directory, operation)
		}
		if operation.SessionID == "" {
			return nil
		}
		var payload forkPayload
		if json.Unmarshal(operation.Payload, &payload) != nil {
			return status_error.ChatLogCorrupted
		}
		if err = m.publishFork(ctx, directory, operation, payload); err != nil {
			return err
		}
		operation.Published = true
		operation.Payload = nil
		return chatstorage.WriteForkOperation(directory, operation)
	})
}

// visitForkOperations 顺序读取持久分叉操作，供恢复和删除共用。
func (m *service) visitForkOperations(visit func(string, chatstorage.ForkOperation) error) error {
	directories, err := os.ReadDir(filepath.Join(m.storage.Root(), constants.ChatForkDirectoryName))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return status_error.ChatLogError
	}
	for _, entry := range directories {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(m.storage.Root(), constants.ChatForkDirectoryName, entry.Name())
		operation, exists, readErr := chatstorage.ReadForkOperation(directory)
		if readErr != nil {
			return readErr
		}
		if !exists {
			continue
		}
		if err = visit(directory, operation); err != nil {
			return err
		}
	}
	return nil
}

// forkLogExists 判断 Chat 是否已有正式权威日志，不把仅有工作区的待发布分支视为公开。
func (m *service) forkLogExists(chatID string) (bool, error) {
	path, err := m.storage.ChatLogDirectory(chatID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// sealVisibleForks 在删除公开子 Chat 前补齐分叉发布标记，避免删后恢复将它重建。
func (m *service) sealVisibleForks(ctx context.Context, chatIDs map[string]struct{}) error {
	return m.visitForkOperations(func(directory string, operation chatstorage.ForkOperation) error {
		if _, deleting := chatIDs[operation.ChatID]; !deleting || operation.Published || operation.Cancelled {
			return nil
		}
		if operation.SessionID == "" {
			return status_error.ChatForkIndeterminate
		}
		var payload forkPayload
		if json.Unmarshal(operation.Payload, &payload) != nil {
			return status_error.ChatLogCorrupted
		}
		if err := m.publishFork(ctx, directory, operation, payload); err != nil {
			return err
		}
		operation.Published = true
		operation.Payload = nil
		return chatstorage.WriteForkOperation(directory, operation)
	})
}

// cancelForksFromDeletedChats 清理来源已删除且尚未公开的子分支。
func (m *service) cancelForksFromDeletedChats(chatIDs map[string]struct{}) error {
	return m.visitForkOperations(func(directory string, operation chatstorage.ForkOperation) error {
		if _, deleting := chatIDs[operation.SourceChatID]; !deleting || operation.Published {
			return nil
		}
		childLog, err := m.forkLogExists(operation.ChatID)
		if err != nil || childLog {
			return err
		}
		return m.cancelUnpublishedFork(directory, operation)
	})
}

// cancelUnpublishedForksForWaker 清理删除伙伴后没有正式 Chat 的遗留分叉意图。
func (m *service) cancelUnpublishedForksForWaker(wakerID string) error {
	return m.visitForkOperations(func(directory string, operation chatstorage.ForkOperation) error {
		if operation.Published || operation.Cancelled {
			return nil
		}
		var payload forkPayload
		if json.Unmarshal(operation.Payload, &payload) != nil {
			return status_error.ChatLogCorrupted
		}
		if payload.Created.WakerID != wakerID {
			return nil
		}
		childLog, err := m.forkLogExists(operation.ChatID)
		if err != nil || childLog {
			return err
		}
		return m.cancelUnpublishedFork(directory, operation)
	})
}

func (m *service) cancelUnpublishedFork(directory string, operation chatstorage.ForkOperation) error {
	if !operation.Cancelled {
		operation.Cancelled = true
		operation.Payload = nil
		if err := chatstorage.WriteForkOperation(directory, operation); err != nil {
			return err
		}
	}
	return m.removeUnpublishedForkFiles(directory, operation.ChatID)
}

func (m *service) removeUnpublishedForkFiles(directory, chatID string) error {
	root, err := m.storage.ChatRoot(chatID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return status_error.ManagedWorkspaceError
	}
	if err := os.RemoveAll(filepath.Join(directory, "staging")); err != nil {
		return status_error.ChatLogError
	}
	if err := os.Remove(filepath.Join(directory, "receipt.json")); err != nil && !os.IsNotExist(err) {
		return status_error.ChatLogError
	}
	return nil
}
