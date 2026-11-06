package chatstorage

import (
	"path/filepath"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// operations 串行化同一受管 Chat 的发送准备、分叉和删除入口。
var operations = struct {
	// Mutex 保护所有正在进行的短期 Chat 操作。
	sync.Mutex
	// busy 按数据根目录和资源身份保存排他操作。
	busy map[string]bool
	// admissions 只统计准备阶段的伙伴共享接纳，不表示原生执行状态。
	admissions map[string]int
}{busy: map[string]bool{}, admissions: map[string]int{}}

// AcquireOperation 尝试独占一组 Chat；不等待，避免与执行收尾形成锁环。
func AcquireOperation(root string, chatIDs ...string) (func(), error) {
	operations.Lock()
	defer operations.Unlock()
	keys := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		if !safeSegment(id) {
			return nil, status_error.BadRequest
		}
		key := filepath.Clean(root) + "\x00" + id
		if operations.busy[key] || operations.admissions[key] > 0 {
			return nil, status_error.ChatHasActiveExecution
		}
		keys[key] = true
	}
	for key := range keys {
		operations.busy[key] = true
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			operations.Lock()
			defer operations.Unlock()
			for key := range keys {
				delete(operations.busy, key)
			}
		})
	}, nil
}

// AcquireAdmission 同时登记伙伴共享接纳和 Chat 排他准备，与伙伴删除共用同一操作门。
func AcquireAdmission(root, wakerID string, chatIDs ...string) (func(), error) {
	if !safeSegment(wakerID) {
		return nil, status_error.BadRequest
	}
	operations.Lock()
	defer operations.Unlock()
	prefix := filepath.Clean(root) + "\x00"
	wakerKey := prefix + "waker:" + wakerID
	if operations.busy[wakerKey] {
		return nil, status_error.ChatHasActiveExecution
	}
	keys := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		if !safeSegment(id) {
			return nil, status_error.BadRequest
		}
		key := prefix + id
		if operations.busy[key] {
			return nil, status_error.ChatHasActiveExecution
		}
		keys[key] = true
	}
	operations.admissions[wakerKey]++
	for key := range keys {
		operations.busy[key] = true
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			operations.Lock()
			defer operations.Unlock()
			for key := range keys {
				delete(operations.busy, key)
			}
			operations.admissions[wakerKey]--
			if operations.admissions[wakerKey] == 0 {
				delete(operations.admissions, wakerKey)
			}
		})
	}, nil
}
