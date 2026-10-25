// Package teamwork 保护团队配置、耐久输入、派发与删除的共同接纳边界。
package teamwork

import (
	"context"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// operationKey 标识同步调用链已经持有的团队保护，不能跨 goroutine 传递持锁 Context。
type operationKey struct{}

// operationLock 为同一团队提供可取消等待的互斥令牌。
type operationLock struct {
	// token 容量为一的令牌只覆盖同步接纳，不覆盖 Runtime 的执行时间。
	token chan struct{}
	// users 统计持有者和等待者，用于在最后一个调用退出时释放索引。
	users int
}

// operations 只登记当前有持有者或等待者的团队锁。
var operations = struct {
	// Mutex 保护锁索引和引用计数，不执行数据库或外部调用。
	sync.Mutex
	// locks 按稳定团队标识隔离操作，不阻塞其他团队。
	locks map[string]*operationLock
}{locks: make(map[string]*operationLock)}

// Acquire 串行保护一个团队的同步写入；同步嵌套调用复用 Context 中的保护。
func Acquire(ctx context.Context, channelID string) (context.Context, func(), error) {
	if ctx.Err() != nil {
		return nil, nil, status_error.OperationCanceled
	}
	held, _ := ctx.Value(operationKey{}).(map[string]bool)
	if held[channelID] {
		return ctx, func() {}, nil
	}
	operations.Lock()
	lock := operations.locks[channelID]
	if lock == nil {
		lock = &operationLock{token: make(chan struct{}, 1)}
		operations.locks[channelID] = lock
	}
	lock.users++
	operations.Unlock()
	releaseReference := func() {
		operations.Lock()
		defer operations.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(operations.locks, channelID)
		}
	}
	select {
	case lock.token <- struct{}{}:
	case <-ctx.Done():
		releaseReference()
		return nil, nil, status_error.OperationCanceled
	}
	next := make(map[string]bool, len(held)+1)
	for id := range held {
		next[id] = true
	}
	next[channelID] = true
	return context.WithValue(ctx, operationKey{}, next), func() { <-lock.token; releaseReference() }, nil
}
