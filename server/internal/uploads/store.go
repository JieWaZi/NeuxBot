// Package uploads 保存主体隔离的短期上传，接纳后由原工作区持有附件。
package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const MaximumBytes = 64 << 20

// entry 保存暂存字节与短期接纳回执路径。
type entry struct {
	// subject 是签发上传的认证主体。
	subject string
	// path 是服务端分配的临时文件位置。
	path string
	// size 用于主体上传总预算。
	size int64
	// acceptedKey 只允许相同幂等请求恢复已消费引用的原路径。
	acceptedKey string
	// busy 表示正在由一个接纳操作读取。
	busy bool
	// removed 表示已过期或主体撤销，等待当前读取释放。
	removed bool
	// timer 管理三十分钟上传有效期。
	timer *time.Timer
	// stopLifetime 释放主体退出监听。
	stopLifetime func() bool
}

// Store 拥有上传字节、预算和一次接纳租约。
type Store struct {
	// mu 保护上传和租约状态。
	mu sync.Mutex
	// root 是当前 Core 独占的临时目录。
	root string
	// entries 以不透明引用索引上传。
	entries map[string]*entry
}

// New 在已锁定的数据根中建立本次 Core 的暂存目录。
func New(dataDirectory string) (*Store, error) {
	old, err := filepath.Glob(filepath.Join(dataDirectory, "web-uploads-*"))
	if err != nil {
		return nil, err
	}
	for _, directory := range old {
		if err := os.RemoveAll(directory); err != nil {
			return nil, err
		}
	}
	root, err := os.MkdirTemp(dataDirectory, "web-uploads-")
	if err != nil {
		return nil, err
	}
	return &Store{root: root, entries: make(map[string]*entry)}, nil
}

// Stage 只接收文件名和有界字节，不接受客户端指定目录。
func (s *Store) Stage(lifetime context.Context, subject, name string, reader io.Reader) (string, error) {
	if subject == "" || name == "" || len(name) > 255 || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("上传文件名无效")
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(bytes[:])
	directory := filepath.Join(s.root, id)
	if err := os.Mkdir(directory, 0700); err != nil {
		return "", err
	}
	filePath := filepath.Join(directory, name)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		os.RemoveAll(directory)
		return "", err
	}
	size, copyErr := io.Copy(file, io.LimitReader(reader, MaximumBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || size > MaximumBytes {
		os.RemoveAll(directory)
		return "", errors.New("上传失败或文件过大")
	}
	s.mu.Lock()
	var total int64
	count := 0
	for _, item := range s.entries {
		if item.subject == subject && item.acceptedKey == "" {
			total += item.size
			count++
		}
	}
	if total+size > 256<<20 || count >= 64 || len(s.entries) >= 2048 {
		s.mu.Unlock()
		os.RemoveAll(directory)
		return "", errors.New("暂存上传已达到上限")
	}
	ref := "upload:" + id + "/" + name
	item := &entry{subject: subject, path: filePath, size: size}
	s.entries[ref] = item
	item.timer = time.AfterFunc(30*time.Minute, func() { s.remove(ref) })
	item.stopLifetime = context.AfterFunc(lifetime, func() { s.remove(ref) })
	s.mu.Unlock()
	return ref, nil
}

func (s *Store) remove(ref string) { s.mu.Lock(); defer s.mu.Unlock(); s.removeLocked(ref) }
func (s *Store) removeLocked(ref string) {
	item := s.entries[ref]
	if item == nil {
		return
	}
	item.removed = true
	if item.busy {
		return
	}
	delete(s.entries, ref)
	item.timer.Stop()
	if item.stopLifetime != nil {
		item.stopLifetime()
	}
	_ = os.RemoveAll(filepath.Dir(item.path))
}

// Claim 原子校验一批引用；同一上传不能并行绑定两个接纳操作。
func (s *Store) Claim(subject string, refs []string, requestKeys ...string) ([]string, func(bool), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requestKey := ""
	if len(requestKeys) > 0 {
		requestKey = requestKeys[0]
	}
	paths := make([]string, len(refs))
	claimed := make(map[string]*entry)
	for index, ref := range refs {
		if !strings.HasPrefix(ref, "upload:") {
			if strings.HasPrefix(subject, "web:") {
				return nil, nil, errors.New("浏览器附件必须先上传")
			}
			paths[index] = ref
			continue
		}
		item := s.entries[ref]
		if item == nil || item.subject != subject || item.busy || item.removed {
			return nil, nil, errors.New("上传已失效或未获授权")
		}
		if item.acceptedKey != "" {
			if requestKey != item.acceptedKey {
				return nil, nil, errors.New("上传已用于其他请求")
			}
			paths[index] = item.path
			continue
		}
		paths[index] = item.path
		claimed[ref] = item
	}
	for _, item := range claimed {
		item.busy = true
	}
	var once sync.Once
	release := func(success bool) {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			for ref, item := range claimed {
				item.busy = false
				if success && requestKey != "" && !item.removed {
					// 字节立即清理；仅保留原路径供业务层校验幂等回执，不允许新请求复用。
					item.acceptedKey = requestKey
					item.size = 0
					_ = os.RemoveAll(filepath.Dir(item.path))
				} else if success || item.removed {
					s.removeLocked(ref)
				}
			}
		})
	}
	return paths, release, nil
}

// Close 删除未接纳上传，不影响已复制到原工作区的附件。
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.entries {
		item.timer.Stop()
		if item.stopLifetime != nil {
			item.stopLifetime()
		}
	}
	s.entries = make(map[string]*entry)
	_ = os.RemoveAll(s.root)
}
