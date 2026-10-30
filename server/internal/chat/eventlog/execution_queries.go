package eventlog

import (
	"encoding/json"
	"hash/crc32"
	"io"
	"os"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ExecutionWindow 是指定执行的生命周期和调用事实，不包含消息正文。
type ExecutionWindow struct {
	// Records 包含 Chat 创建及所选执行的状态事实。
	Records []Record
	// StartSequence 是所选执行创建事实的持久序号。
	StartSequence int64
	// LatestSequence 是本次读取已覆盖的原日志游标。
	LatestSequence int64
}

// ExecutionFactPositions 只引用已经被原日志定位投影覆盖的事实位置。
type ExecutionFactPositions struct {
	// StartSequence 是 execution.created 的原序号。
	StartSequence int64
	// LifecycleSequences 是 execution.started 与终态的原序号。
	LifecycleSequences []int64
	// InvocationFinishedSequence 是最后一次持久调用结论的原序号。
	InvocationFinishedSequence int64
	// InvocationBoundarySequence 是该调用身份首次出现的原信封序号。
	InvocationBoundarySequence int64
	// LatestSequence 是上述定位完整覆盖的日志游标。
	LatestSequence int64
}

// ExecutionIndexPosition 保存一次未覆盖尾部中的事实位置，不包含正文或执行状态。
type ExecutionIndexPosition struct {
	// Sequence 是原日志记录的位置。
	Sequence int64
	// ExecutionStartSequence 是所属 execution.created 的位置。
	ExecutionStartSequence int64
	// Lifecycle 表示执行启动或终态事实。
	Lifecycle bool
	// InvocationBoundary 表示原生调用身份在该信封首次出现。
	InvocationBoundary bool
	// InvocationFinished 表示该记录是持久调用结论。
	InvocationFinished bool
}

// ExecutionIndexTail 保存定位投影尚未覆盖的原索引尾部。
type ExecutionIndexTail struct {
	// Records 只包含消息入口信封及涉及执行的创建配置。
	Records []Record
	// Positions 是尾部新增的生命周期与调用位置。
	Positions []ExecutionIndexPosition
	// LatestSequence 是该尾部已覆盖的原日志游标。
	LatestSequence int64
}

// ReadExecutionFacts 只随机读取已经定位的生命周期和最后调用事实，不扫描消息索引。
func (s *Store) ReadExecutionFacts(chatID string, positions ExecutionFactPositions) (ExecutionWindow, error) {
	state, err := s.state(chatID)
	if err != nil {
		return ExecutionWindow{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() (ExecutionWindow, error) {
		if positions.LatestSequence > state.lastSequence {
			return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
		}
		index, err := os.Open(s.indexPath(chatID))
		if err != nil {
			return ExecutionWindow{}, status_error.ChatLogError
		}
		defer index.Close()
		log, err := os.Open(s.path(chatID))
		if err != nil {
			return ExecutionWindow{}, status_error.ChatLogError
		}
		defer log.Close()
		return readExecutionFactWindow(chatID, positions, index, log)
	})
}

// readExecutionFactWindow 按已覆盖原序号随机读取，正文条目增长不增加状态查询 I/O。
func readExecutionFactWindow(chatID string, positions ExecutionFactPositions, index, log io.ReaderAt) (ExecutionWindow, error) {
	sequences := []int64{1}
	if positions.StartSequence > 0 {
		sequences = append(sequences, positions.StartSequence)
		sequences = append(sequences, positions.LifecycleSequences...)
		if positions.InvocationFinishedSequence > 0 {
			sequences = append(sequences, positions.InvocationFinishedSequence)
		}
		if positions.InvocationBoundarySequence > 0 {
			sequences = append(sequences, positions.InvocationBoundarySequence)
		}
	}
	slices.Sort(sequences)
	sequences = slices.Compact(sequences)
	result := ExecutionWindow{StartSequence: positions.StartSequence, LatestSequence: positions.LatestSequence}
	scope := recordScope{}
	for _, sequence := range sequences {
		if sequence < 1 || sequence > positions.LatestSequence {
			return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
		}
		entry, err := readIndexEntryAt(index, sequence)
		if err != nil {
			return ExecutionWindow{}, err
		}
		if sequence == 1 && entry.flags&indexFlagChatCreated == 0 {
			return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
		}
		if sequence == positions.StartSequence && entry.flags&indexFlagAssistantMessage == 0 {
			return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
		}
		if sequence > 1 && entry.executionStartSequence != positions.StartSequence {
			return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
		}
		size := entry.length
		headerOnly := entry.flags&indexFlagExecutionFact == 0 && sequence != 1
		if headerOnly {
			if sequence != positions.InvocationBoundarySequence || entry.flags&indexFlagInvocationBoundary == 0 {
				return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
			}
			size = int64(entry.headerLength)
		}
		line := make([]byte, size)
		if _, err := log.ReadAt(line, entry.offset); err != nil {
			return ExecutionWindow{}, status_error.ChatLogError
		}
		if headerOnly {
			var header persistedRecord
			if json.Unmarshal(append(line, '}'), &header) != nil {
				return ExecutionWindow{}, status_error.ChatLogCorrupted
			}
			scope.invocationID = header.InvocationID
			continue
		}
		if crc32.ChecksumIEEE(line) != entry.checksum {
			return ExecutionWindow{}, status_error.ChatLogIndexCorrupted
		}
		record, err := decodeRecordPayload(line[:len(line)-1], chatID, sequence, "execution fact", &scope)
		if err != nil {
			return ExecutionWindow{}, err
		}
		record.Offset, record.Length = entry.offset, entry.length
		result.Records = append(result.Records, record)
	}
	return result, nil
}

// ReadExecutionIndexTail 只扫描未覆盖的原索引尾部，生命周期位置只在此增量推进一次。
func (s *Store) ReadExecutionIndexTail(chatID string, after int64) (ExecutionIndexTail, error) {
	state, err := s.state(chatID)
	if err != nil {
		return ExecutionIndexTail{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() (ExecutionIndexTail, error) {
		index, err := os.Open(s.indexPath(chatID))
		if err != nil {
			return ExecutionIndexTail{}, status_error.ChatLogError
		}
		defer index.Close()
		log, err := os.Open(s.path(chatID))
		if err != nil {
			return ExecutionIndexTail{}, status_error.ChatLogError
		}
		defer log.Close()
		result := ExecutionIndexTail{LatestSequence: state.lastSequence}
		starts := map[int64]bool{}
		var readErr error
		err = scanIndexEntries(index, after+1, state.lastSequence, func(entry indexEntry) bool {
			if entry.isMessageStart() {
				var records []Record
				if entry.flags&indexFlagAssistantMessage != 0 {
					records, readErr = s.readRangeLocked(chatID, entry.offset, entry.endOffset(), entry.sequence)
					starts[entry.sequence] = true
				} else {
					line := make([]byte, int(entry.headerLength)+1)
					_, readErr = log.ReadAt(line[:len(line)-1], entry.offset)
					line[len(line)-1] = '}'
					if readErr == nil {
						var record Record
						record, readErr = decodeRecordPayload(line, chatID, entry.sequence, "message index envelope", &recordScope{})
						records = []Record{record}
					}
				}
				if readErr != nil {
					return false
				}
				result.Records = append(result.Records, records...)
			}
			if entry.flags&(indexFlagExecutionFact|indexFlagInvocationBoundary) != 0 && entry.executionStartSequence > 0 {
				result.Positions = append(result.Positions, ExecutionIndexPosition{Sequence: entry.sequence, ExecutionStartSequence: entry.executionStartSequence, Lifecycle: entry.flags&indexFlagExecutionFact != 0 && entry.flags&(indexFlagInvocationFinished|indexFlagAssistantMessage) == 0, InvocationBoundary: entry.flags&indexFlagInvocationBoundary != 0, InvocationFinished: entry.flags&indexFlagInvocationFinished != 0})
				if _, ok := starts[entry.executionStartSequence]; !ok {
					starts[entry.executionStartSequence] = false
				}
			}
			return true
		})
		if err != nil {
			return ExecutionIndexTail{}, err
		}
		if readErr != nil {
			return ExecutionIndexTail{}, readErr
		}
		for sequence, read := range starts {
			if read {
				continue
			}
			entry, err := readIndexEntryAt(index, sequence)
			if err != nil {
				return ExecutionIndexTail{}, err
			}
			records, err := s.readRangeLocked(chatID, entry.offset, entry.endOffset(), sequence)
			if err != nil {
				return ExecutionIndexTail{}, err
			}
			if len(records) != 1 || records[0].Type != constants.ChatEventTypeExecutionCreated {
				return ExecutionIndexTail{}, status_error.ChatLogIndexCorrupted
			}
			result.Records = append(result.Records, records...)
		}
		return result, nil
	})
}

// HistoryCandidate 是原索引定位的成功执行或继承消息，尚未读取消息正文。
type HistoryCandidate struct {
	// MessageID 是原执行的助手身份或继承消息身份。
	MessageID string
	// UserMessageID 是真实执行关联的原用户输入身份，继承消息为空。
	UserMessageID string
	// Sequence 是该执行或消息在原日志中的创建位置。
	Sequence int64
	// Inherited 表示候选来自独立继承消息而非本 Chat 执行。
	Inherited bool
}

// HistoryCandidateWindow 是最近成功完整轮次的有界候选页。
type HistoryCandidateWindow struct {
	// Resources 按原创建顺序倒序返回候选。
	Resources []HistoryCandidate
	// NextBeforeSequence 是继续查找更早候选的原位置游标。
	NextBeforeSequence int64
	// HasMore 表示仍有更早原索引位置可检查。
	HasMore bool
}

// ReadCompletedHistoryCandidates 跳过整个失败或未完成执行，只定位成功轮及继承消息，不读取工具或正文。
func (s *Store) ReadCompletedHistoryCandidates(chatID string, before int64, limit int) (HistoryCandidateWindow, error) {
	if before < 0 || limit < 1 {
		return HistoryCandidateWindow{}, status_error.BadRequest
	}
	state, err := s.state(chatID)
	if err != nil {
		return HistoryCandidateWindow{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() (HistoryCandidateWindow, error) {
		file, err := os.Open(s.indexPath(chatID))
		if err != nil {
			return HistoryCandidateWindow{}, status_error.ChatLogError
		}
		defer file.Close()
		log, err := os.Open(s.path(chatID))
		if err != nil {
			return HistoryCandidateWindow{}, status_error.ChatLogError
		}
		defer log.Close()
		return readCompletedHistoryCandidates(chatID, before, limit, state.lastSequence, file, log)
	})
}

// readCompletedHistoryCandidates 只读取执行入口与终态定位，直接越过全部过程条目。
func readCompletedHistoryCandidates(chatID string, before int64, limit int, latest int64, file, log io.ReaderAt) (HistoryCandidateWindow, error) {
	sequence := latest
	if before > 0 {
		sequence = min(sequence, before-1)
	}
	result := HistoryCandidateWindow{}
	for sequence > 1 && len(result.Resources) < limit {
		entry, err := readIndexEntryAt(file, sequence)
		if err != nil {
			return HistoryCandidateWindow{}, err
		}
		if entry.flags&indexFlagInheritedMessage != 0 {
			line := make([]byte, int(entry.headerLength)+1)
			if _, err := log.ReadAt(line[:len(line)-1], entry.offset); err != nil {
				return HistoryCandidateWindow{}, status_error.ChatLogError
			}
			line[len(line)-1] = '}'
			record, err := decodeRecordPayload(line, chatID, sequence, "inherited history envelope", &recordScope{})
			if err != nil {
				return HistoryCandidateWindow{}, err
			}
			result.Resources = append(result.Resources, HistoryCandidate{MessageID: record.MessageID, Sequence: sequence, Inherited: true})
			sequence--
			continue
		}

		if entry.executionStartSequence > 0 {
			start, err := readIndexEntryAt(file, entry.executionStartSequence)
			if err != nil {
				return HistoryCandidateWindow{}, err
			}
			if entry.flags&indexFlagExecutionTerminal != 0 {
				terminal, err := readHistoryCandidateRecord(chatID, entry, log)
				if err != nil {
					return HistoryCandidateWindow{}, err
				}
				var finished ExecutionFinishedData
				if len(terminal) != 1 || json.Unmarshal(terminal[0].Data, &finished) != nil {
					return HistoryCandidateWindow{}, status_error.ChatLogCorrupted
				}
				if finished.Status == constants.ExecutionStatusCompleted {
					created, err := readHistoryCandidateRecord(chatID, start, log)
					if err != nil {
						return HistoryCandidateWindow{}, err
					}
					if len(created) != 1 || created[0].Type != constants.ChatEventTypeExecutionCreated {
						return HistoryCandidateWindow{}, status_error.ChatLogCorrupted
					}
					var input struct {
						// UserMessageID 是执行创建时冻结的输入身份。
						UserMessageID string `json:"userMessageId"`
					}
					if json.Unmarshal(created[0].Data, &input) != nil {
						return HistoryCandidateWindow{}, status_error.ChatLogCorrupted
					}
					result.Resources = append(result.Resources, HistoryCandidate{MessageID: created[0].ExecutionID, UserMessageID: input.UserMessageID, Sequence: start.sequence})
				}
			}
			// executionStartSequence 直接越过该执行全部工具与正文索引条目。
			sequence = start.sequence - 1
			continue
		}
		sequence--
	}
	result.HasMore = sequence > 1
	if result.HasMore {
		result.NextBeforeSequence = sequence + 1
	}
	return result, nil
}

// readHistoryCandidateRecord 读取候选入口或终态单条权威记录，不展开其他日志范围。
func readHistoryCandidateRecord(chatID string, entry indexEntry, log io.ReaderAt) ([]Record, error) {
	line := make([]byte, entry.length)
	if _, err := log.ReadAt(line, entry.offset); err != nil {
		return nil, status_error.ChatLogError
	}
	if crc32.ChecksumIEEE(line) != entry.checksum {
		return nil, status_error.ChatLogIndexCorrupted
	}
	return decodeRecordRange(chatID, entry.offset, entry.sequence, line)
}
