// Package agentcommand 将 CLI 请求身份带入领域原有事务，不拥有业务状态。
package agentcommand

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
)

// Receipt 是与领域变更一同提交的最小接纳事实。
type Receipt struct {
	// ID 对同一作用域、操作和请求保持稳定。
	ID string
	// Digest 拒绝相同请求身份的异内容重放。
	Digest string
	// Data 记录首次接纳结果，不保存凭据或完整输入。
	Data map[string]any
}

// Store 必须使用传入 Context 中领域事务的同一连接。
type Store interface {
	Find(context.Context, string) (Receipt, bool, error)
	Create(context.Context, Receipt) error
}

// pending 只在当前调用栈传递待提交事实。
type pending struct {
	// store 是业务数据库的回执仓储。
	store Store
	// receipt 固定身份和输入摘要。
	receipt Receipt
}

type contextKey struct{}

// Active 表示当前调用必须在自身领域事务内提交接纳事实。
func Active(ctx context.Context) bool { _, ok := ctx.Value(contextKey{}).(pending); return ok }

// ID 根据服务端拥有的作用域计算不可跨场景复用的命令身份。
func ID(scope, operation, requestID string) string {
	encoded, _ := json.Marshal([]string{scope, operation, requestID})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// Lookup 读取已接纳事实，不尝试重新执行命令。
func Lookup(ctx context.Context, store Store, scope, operation, requestID string) (agentcapability.Result, error) {
	if store == nil {
		return agentcapability.Result{}, status_error.AgentConnectionUnavailable
	}
	receipt, found, err := store.Find(ctx, ID(scope, operation, requestID))
	if err != nil {
		return agentcapability.Result{}, err
	}
	if !found {
		return agentcapability.Result{}, status_error.AgentReceiptNotFound
	}
	return agentcapability.Result{CommandID: receipt.ID, Replayed: true, Data: receipt.Data}, nil
}

// Execute 先重放，再调用领域命令；只有同库事务内的 Commit 才构成成功。
func Execute(ctx context.Context, store Store, scope string, request agentcapability.Request, call func(context.Context) error) (agentcapability.Result, error) {
	if store == nil {
		return agentcapability.Result{}, status_error.AgentConnectionUnavailable
	}
	encoded, err := json.Marshal(request.Params)
	if len(request.InputDigests) > 0 {
		encoded, err = json.Marshal([]any{request.Params, request.InputDigests})
	}
	if err != nil {
		return agentcapability.Result{}, status_error.AgentInputInvalid
	}
	hash := sha256.Sum256(encoded)
	wanted := Receipt{ID: ID(scope, request.Operation, request.RequestID), Digest: hex.EncodeToString(hash[:])}
	read := func() (agentcapability.Result, bool, error) {
		receipt, found, err := store.Find(ctx, wanted.ID)
		if err != nil || !found {
			return agentcapability.Result{}, found, err
		}
		if receipt.Digest != wanted.Digest {
			return agentcapability.Result{}, true, status_error.AgentCommandRequestConflict
		}
		return agentcapability.Result{CommandID: receipt.ID, Replayed: true, Data: receipt.Data}, true, nil
	}
	if result, found, err := read(); err != nil || found {
		return result, err
	}
	callErr := call(context.WithValue(ctx, contextKey{}, pending{store: store, receipt: wanted}))
	result, found, err := read()
	if err != nil {
		if errors.Is(err, status_error.AgentCommandRequestConflict) {
			return agentcapability.Result{}, err
		}
		return agentcapability.Result{}, status_error.AgentCommandOutcomeUnknown
	}
	if found {
		// 即使提交后的派发或投影失败，原接纳事实仍然成立。
		result.Replayed = callErr != nil
		return result, nil
	}
	if callErr != nil {
		return agentcapability.Result{}, callErr
	}
	return agentcapability.Result{}, status_error.AgentCommandOutcomeUnknown
}

// Commit 由领域在自身目标变更事务内显式调用；普通业务入口不产生 CLI 回执。
func Commit(ctx context.Context, data map[string]any) error {
	value, ok := ctx.Value(contextKey{}).(pending)
	if !ok {
		return nil
	}
	value.receipt.Data = data
	return value.store.Create(ctx, value.receipt)
}
