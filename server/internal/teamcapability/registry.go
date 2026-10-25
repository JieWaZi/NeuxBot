// Package channeltool 封装 Team 协作 MCP 的上下文授权与回环传输。
package teamcapability

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Scope 是一次团队执行调用协作工具时的授权范围。
type Scope struct {
	// ThreadID 是工具能够读取和修改的唯一团队对话。
	ThreadID string
	// CallerWakerID 是获得该能力的团队伙伴标识。
	CallerWakerID string
	// ExecutionID 是当前唯一允许调用该端点的团队执行。
	ExecutionID string
	// Fence 是同一 Team 对话能力激活的单调代次。
	Fence uint64
	// Role 是当前能力绑定的稳定 Team 角色。
	Role string
	// TaskID 是成员报告能力绑定的唯一任务标识；负责人能力为空。
	TaskID string
	// AttemptID 是成员报告能力绑定的唯一尝试标识；负责人能力为空。
	AttemptID string
	// Listed 表示本轮是否已读取任务板。
	Listed bool
	// AllowMutations 表示本轮是否允许维护任务。
	AllowMutations bool
	// ExpiresAt 是本轮能力强制失效的时间。
	ExpiresAt time.Time
	// LeaseID 是生命周期持有者撤销本代能力时使用的随机标识。
	LeaseID string
}

// Registry 按稳定 Team 角色端点保存当前 execution-scoped 授权。
type Registry struct {
	// mutex 保护授权映射和单调代次。
	mutex sync.Mutex
	// byEndpoint 按 thread 或 thread+member 稳定键保存当前执行授权。
	byEndpoint map[string]Scope
	// nextFence 保存下一次激活使用的单调代次。
	nextFence uint64
}

// NewRegistry 创建空的团队工具授权注册表。
func NewRegistry() *Registry {
	return &Registry{byEndpoint: make(map[string]Scope)}
}

// Mint 为一次负责人执行签发任务板能力。
func (r *Registry) Mint(
	threadID string,
	callerWakerID string,
	executionID string,
	allowMutations bool,
) (string, error) {
	return r.mint(threadID, Scope{
		ThreadID:       threadID,
		CallerWakerID:  callerWakerID,
		ExecutionID:    executionID,
		Role:           constants.ChannelCapabilityRoleLead,
		AllowMutations: allowMutations,
	})
}

// MintMember 为一次成员任务尝试签发唯一工作报告能力。
func (r *Registry) MintMember(
	threadID string,
	callerWakerID string,
	executionID string,
	taskID string,
	attemptID string,
) (string, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" {
		return "", status_error.BadRequest
	}
	return r.mint(MemberEndpointKey(threadID, callerWakerID), Scope{
		ThreadID:       threadID,
		CallerWakerID:  callerWakerID,
		ExecutionID:    executionID,
		Role:           constants.ChannelCapabilityRoleMember,
		TaskID:         taskID,
		AttemptID:      attemptID,
		AllowMutations: true,
	})
}

// MemberEndpointKey 返回同一团队对话成员跨任务复用的稳定能力端点键。
func MemberEndpointKey(threadID string, callerWakerID string) string {
	return strings.TrimSpace(threadID) + "-" + strings.TrimSpace(callerWakerID)
}

// mint 校验公共执行身份并保存不可猜测的短期能力。
func (r *Registry) mint(endpointKey string, scope Scope) (string, error) {
	threadID := strings.TrimSpace(scope.ThreadID)
	callerWakerID := strings.TrimSpace(scope.CallerWakerID)
	executionID := strings.TrimSpace(scope.ExecutionID)
	endpointKey = strings.TrimSpace(endpointKey)
	if endpointKey == "" || strings.TrimSpace(threadID) == "" || strings.TrimSpace(callerWakerID) == "" ||
		strings.TrimSpace(executionID) == "" {
		return "", status_error.BadRequest
	}
	random := make([]byte, constants.ChannelTasksMCPTokenBytes)
	if _, err := rand.Read(random); err != nil {
		logx.Errorf("mint channel tool capability failed: thread_id=%s execution_id=%s error=%v", threadID, executionID, err)
		return "", status_error.CoreChannelInitializationFailed
	}
	leaseID := hex.EncodeToString(random)
	r.mutex.Lock()
	r.nextFence++
	scope.ThreadID = threadID
	scope.CallerWakerID = callerWakerID
	scope.ExecutionID = executionID
	scope.TaskID = strings.TrimSpace(scope.TaskID)
	scope.AttemptID = strings.TrimSpace(scope.AttemptID)
	scope.Fence = r.nextFence
	scope.ExpiresAt = time.Now().Add(time.Duration(constants.ChannelCapabilityTTLHours) * time.Hour)
	scope.LeaseID = leaseID
	r.byEndpoint[endpointKey] = scope
	r.mutex.Unlock()
	return leaseID, nil
}

// Resolve 返回稳定角色端点当前绑定的执行授权；未知、过期或已撤销时返回 false。
func (r *Registry) Resolve(endpointKey string) (Scope, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	scope, exists := r.byEndpoint[endpointKey]
	if !exists {
		return Scope{}, false
	}
	if !scope.ExpiresAt.After(time.Now()) {
		delete(r.byEndpoint, endpointKey)
		return Scope{}, false
	}
	return scope, true
}

// MarkListed 记录当前负责人执行已经读取任务板。
func (r *Registry) MarkListed(endpointKey string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	scope, exists := r.byEndpoint[endpointKey]
	if !exists || !scope.ExpiresAt.After(time.Now()) {
		return
	}
	scope.Listed = true
	r.byEndpoint[endpointKey] = scope
}

// Revoke 只撤销与 lease 精确匹配的能力，旧 watcher 不会撤销新一代授权。
func (r *Registry) Revoke(leaseID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for endpointKey, scope := range r.byEndpoint {
		if scope.LeaseID == leaseID {
			delete(r.byEndpoint, endpointKey)
			return
		}
	}
}

// Close 清空进程内全部团队工具能力。
func (r *Registry) Close() {
	r.mutex.Lock()
	r.byEndpoint = make(map[string]Scope)
	r.mutex.Unlock()
}
