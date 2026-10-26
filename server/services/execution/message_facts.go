package execution

import (
	"context"
	"sort"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// executionMessageFacts 将已提交的执行消息事实交给唯一实时投影，不持有协调器或 Sink 的锁。
type executionMessageFacts struct {
	// mutex 保护提交者与实时消费者之间的事实交接。
	mutex sync.Mutex
	// pending 只保存实时投影尚未消费的权威事实，不复制消息状态。
	pending []eventlog.Record
	// invocationID 仅为当前执行的 ally 问题绑定 Runtime 调用；原生交互使用 Bridge 明确提供的身份。
	invocationID string
	// wake 合并已提交事实的交付唤醒，执行期间最多保留一个待处理通知。
	wake chan struct{}
	// stop 由执行所有者关闭，结束唯一事实交付 worker。
	stop chan struct{}
	// done 确认 worker 已退出，终态 Flush 前必须等待。
	done chan struct{}
	// stopOnce 保证正常结束和清理路径可以重复停止同一 worker。
	stopOnce sync.Once
}

// start 由执行所有者显式启动；worker 只安排既有快照窗口，不复制或持有消息状态。
func (f *executionMessageFacts) start(deliver func()) {
	if f == nil {
		return
	}
	f.mutex.Lock()
	if f.wake != nil {
		f.mutex.Unlock()
		return
	}
	f.wake = make(chan struct{}, 1)
	f.stop = make(chan struct{})
	f.done = make(chan struct{})
	wake, stop, done := f.wake, f.stop, f.done
	if len(f.pending) > 0 {
		wake <- struct{}{}
	}
	f.mutex.Unlock()
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-wake:
				select {
				case <-stop:
					return
				default:
					deliver()
				}
			}
		}
	}()
}

// stopDelivery 必须在 Sink 锁之外调用；等待退出后由原 Sink Flush 追平全部余量。
func (f *executionMessageFacts) stopDelivery() {
	if f == nil {
		return
	}
	f.mutex.Lock()
	stop, done := f.stop, f.done
	if stop == nil {
		f.mutex.Unlock()
		return
	}
	f.stopOnce.Do(func() { close(stop) })
	f.mutex.Unlock()
	<-done
}

func (f *executionMessageFacts) hasPending() bool {
	if f == nil {
		return false
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.pending) > 0
}

// push 只接收已成功持久化的事实，不同步回调实时消费者，避免提交锁与 Sink 锁倒置。
func (f *executionMessageFacts) push(records ...eventlog.Record) {
	if f == nil {
		return
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	for _, record := range records {
		record.Data = append([]byte(nil), record.Data...)
		f.pending = append(f.pending, record)
	}
	if f.wake != nil {
		select {
		case f.wake <- struct{}{}:
		default:
		}
	}
}

func (f *executionMessageFacts) take() []eventlog.Record {
	if f == nil {
		return nil
	}
	f.mutex.Lock()
	result := f.pending
	f.pending = nil
	f.mutex.Unlock()
	// 审批、问答和 Core 命令可由独立提交者完成；同一批次按权威序号交付。
	sort.SliceStable(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result
}

func (f *executionMessageFacts) setInvocation(id string) {
	if f == nil {
		return
	}
	f.mutex.Lock()
	f.invocationID = id
	f.mutex.Unlock()
}

func (f *executionMessageFacts) invocation() string {
	if f == nil {
		return ""
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.invocationID
}

// record 先提交 Core 命令事实，再交接给实时投影；失败提交不进入队列。
func (f *executionMessageFacts) record(ctx context.Context, journal chatservice.Journal, chatID, executionID string, op contract.Operation, result agentcapability.Result) error {
	records, err := journal.AppendExecutionFacts(ctx, chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeAgentCommandCommitted, ExecutionID: executionID, Data: marshalData(eventlog.AgentCommandData{CommandID: result.CommandID, Operation: op.ID, Completion: op.Completion})}})
	if err == nil {
		f.push(records...)
	}
	return err
}
