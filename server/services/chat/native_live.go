package chat

import (
	"context"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	nativeLiveWindow = 12 * time.Millisecond
	nativeLiveInputs = 128
)

// NativeLiveMessage 把本轮的规范化文本投影为现有聊天协议的临时增量。
type NativeLiveMessage struct {
	// mu 串行化原生输入、检查点、定时交付和关闭。
	mu sync.Mutex
	// chat 是实时交付与权威检查点的所有者。
	chat *service
	// chatID 标识订阅所属聊天。
	chatID string
	// adapter 保存单轮的展示部件及待交付增量。
	adapter *eventstream.LiveAdapter
	// timer 限制首个待交付增量的等待时间。
	timer *time.Timer
}

// BeginNativeLive 从实际开始事实初始化投影，复用当前聊天订阅。
func (m *service) BeginNativeLive(chatID, messageID string, startedRevision int64) *NativeLiveMessage {
	return &NativeLiveMessage{chat: m, chatID: chatID, adapter: eventstream.NewNativeLiveAdapter(messageID, startedRevision)}
}

// Append 将本次文本更新放入有界短窗口。
func (l *NativeLiveMessage) Append(change agent.MessageChange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.adapter == nil {
		return
	}
	if err := l.adapter.AppendNative(change, time.Now().UnixMilli()); err != nil {
		l.disable(err)
		return
	}
	if l.adapter.PendingCount() >= nativeLiveInputs {
		l.flush()
	} else if l.timer == nil {
		var timer *time.Timer
		timer = time.AfterFunc(nativeLiveWindow, func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.timer == timer {
				l.flush()
			}
		})
		l.timer = timer
	}
}

// Checkpoint 先交付旧窗口，再公布覆盖它的持久版本并更新下一帧基线。
func (l *NativeLiveMessage) Checkpoint(ctx context.Context, receipt agent.CommitReceipt, change agent.MessageChange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flush()
	if l.adapter != nil {
		var err error
		switch {
		case l.adapter.NativeCheckpointNeedsRebase(change):
			journal := l.chat.logStore.(*conversationJournal)
			var stored agent.StoredMessage
			var checkpoint int64
			stored, checkpoint, err = journal.ReadMessageCheckpoint(ctx, l.chatID, change.MessageID, receipt.Version)
			if err == nil && checkpoint != receipt.Version {
				err = status_error.ChatLogError
			}
			if err == nil {
				err = l.adapter.RebaseNative(stored, receipt.Version, receipt.RecordedAt)
			}
		case change.Kind != agent.MessageAppend:
			err = l.adapter.AppendNative(change, receipt.RecordedAt)
		}
		if err != nil {
			l.disable(err)
		}
	}
	l.chat.subscriptions.publishMessageCheckpoint(l.chatID, receipt.Version, change.MessageID, change.Revision)
}

// Close 在业务终态发布前停止本轮实时交付。
func (l *NativeLiveMessage) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopTimer()
	l.adapter = nil
}

func (l *NativeLiveMessage) stopTimer() {
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

func (l *NativeLiveMessage) flush() {
	l.stopTimer()
	for l.adapter != nil && l.adapter.PendingCount() > 0 {
		event, err := l.adapter.Drain()
		if err != nil {
			l.disable(err)
			return
		}
		l.chat.PublishLive(l.chatID, event)
	}
}

func (l *NativeLiveMessage) disable(err error) {
	l.stopTimer()
	l.adapter = nil
	logx.Errorf("project native live message: chat_id=%s error=%v", l.chatID, err)
}
