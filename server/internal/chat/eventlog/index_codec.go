package eventlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	indexSchemaVersion   = uint32(3)
	indexHeaderSize      = int64(64)
	indexEntrySize       = int64(88)
	indexFlagChatCreated = uint32(1 << iota)
	indexFlagUserMessage
	indexFlagAssistantMessage
	indexFlagExecutionTerminal
	indexFlagInheritedMessage
	indexFlagExecutionFact
	indexFlagInvocationBoundary
	indexFlagInvocationFinished
)

var indexMagic = [8]byte{'A', 'L', 'L', 'Y', 'I', 'D', 'X', '1'}

// indexEntry 是一条 JSONL 记录对应的固定长度派生定位信息。
type indexEntry struct {
	// sequence 是对应 JSONL 记录的一基连续序号。
	sequence int64
	// offset 是对应 JSONL 行的起始字节位置。
	offset int64
	// length 是对应 JSONL 行包含换行符的字节数。
	length int64
	// recordedAt 是对应事实提交时的 Unix 毫秒时间。
	recordedAt int64
	// executionStartSequence 是当前执行作用域入口的序号，作用域外为零。
	executionStartSequence int64
	// checksum 是完整 JSONL 行的 CRC32 校验值。
	checksum uint32
	// flags 标记消息与执行入口、生命周期、调用边界和 Chat 创建记录。
	flags uint32
	// messageHash 是消息入口稳定标识的 SHA-256 摘要。
	messageHash [32]byte
	// headerLength 是不含正文的信封头部字节数，用于恢复调用身份。
	headerLength uint32
}

// encodeIndexHeader 编码固定长度索引头。
func encodeIndexHeader(chatID string) ([]byte, error) {
	if !validChatID(chatID) {
		return nil, status_error.ChatIDInvalid
	}
	buffer := make([]byte, indexHeaderSize)
	copy(buffer[0:8], indexMagic[:])
	binary.LittleEndian.PutUint32(buffer[8:12], indexSchemaVersion)
	binary.LittleEndian.PutUint32(buffer[12:16], uint32(indexEntrySize))
	copy(buffer[16:48], chatID)
	return buffer, nil
}

// validateIndexHeader 校验索引格式与 Chat 文件身份。
func validateIndexHeader(buffer []byte, chatID string) error {
	if len(buffer) != int(indexHeaderSize) {
		return status_error.ChatLogIndexCorrupted
	}
	if string(buffer[0:8]) != string(indexMagic[:]) ||
		binary.LittleEndian.Uint32(buffer[8:12]) != indexSchemaVersion ||
		binary.LittleEndian.Uint32(buffer[12:16]) != uint32(indexEntrySize) ||
		string(buffer[16:48]) != chatID {
		return status_error.ChatLogIndexCorrupted
	}
	return nil
}

// encodeIndexEntry 编码固定长度索引记录。
func encodeIndexEntry(entry indexEntry) []byte {
	buffer := make([]byte, indexEntrySize)
	binary.LittleEndian.PutUint64(buffer[0:8], uint64(entry.sequence))
	binary.LittleEndian.PutUint64(buffer[8:16], uint64(entry.offset))
	binary.LittleEndian.PutUint64(buffer[16:24], uint64(entry.length))
	binary.LittleEndian.PutUint64(buffer[24:32], uint64(entry.recordedAt))
	binary.LittleEndian.PutUint64(buffer[32:40], uint64(entry.executionStartSequence))
	binary.LittleEndian.PutUint32(buffer[40:44], entry.checksum)
	binary.LittleEndian.PutUint32(buffer[44:48], entry.flags)
	copy(buffer[48:80], entry.messageHash[:])
	binary.LittleEndian.PutUint32(buffer[80:84], entry.headerLength)
	binary.LittleEndian.PutUint32(buffer[84:88], crc32.ChecksumIEEE(buffer[:84]))
	return buffer
}

// decodeIndexEntry 解码并校验一条固定长度索引记录。
func decodeIndexEntry(buffer []byte) (indexEntry, error) {
	if len(buffer) != int(indexEntrySize) {
		return indexEntry{}, status_error.ChatLogIndexCorrupted
	}
	if binary.LittleEndian.Uint32(buffer[84:88]) != crc32.ChecksumIEEE(buffer[:84]) {
		return indexEntry{}, status_error.ChatLogIndexCorrupted
	}
	entry := indexEntry{
		sequence:               int64(binary.LittleEndian.Uint64(buffer[0:8])),
		offset:                 int64(binary.LittleEndian.Uint64(buffer[8:16])),
		length:                 int64(binary.LittleEndian.Uint64(buffer[16:24])),
		recordedAt:             int64(binary.LittleEndian.Uint64(buffer[24:32])),
		executionStartSequence: int64(binary.LittleEndian.Uint64(buffer[32:40])),
		checksum:               binary.LittleEndian.Uint32(buffer[40:44]),
		flags:                  binary.LittleEndian.Uint32(buffer[44:48]),
	}
	copy(entry.messageHash[:], buffer[48:80])
	entry.headerLength = binary.LittleEndian.Uint32(buffer[80:84])
	if entry.sequence < 1 || entry.offset < 0 || entry.length < 1 || entry.headerLength == 0 || int64(entry.headerLength) >= entry.length ||
		entry.executionStartSequence < 0 ||
		entry.executionStartSequence > entry.sequence {
		return indexEntry{}, status_error.ChatLogIndexCorrupted
	}
	return entry, nil
}

// indexEntryForRecord 从已经编码完成的权威记录派生索引记录。
func indexEntryForRecord(record Record, executionStartSequence int64, line []byte) indexEntry {
	entry := indexEntry{
		sequence:               record.Sequence,
		offset:                 record.Offset,
		length:                 record.Length,
		recordedAt:             record.RecordedAt,
		executionStartSequence: executionStartSequence,
		checksum:               crc32.ChecksumIEEE(line),
	}
	headerEnd := bytes.Index(line, []byte(`,"data":`))
	if headerEnd < 0 {
		headerEnd = len(line) - 2
	}
	entry.headerLength = uint32(headerEnd)
	if bytes.Contains(line[:headerEnd], []byte(`"invocationId":`)) {
		entry.flags |= indexFlagInvocationBoundary
	}
	switch record.Type {
	case constants.ChatEventTypeCreated:
		entry.flags |= indexFlagChatCreated
	case constants.ChatEventTypeMessageInherited:
		entry.flags |= indexFlagInheritedMessage
		entry.messageHash = sha256.Sum256([]byte(record.MessageID))
	case constants.ChatEventTypeUserMessageCreated:
		entry.flags |= indexFlagUserMessage
		entry.messageHash = sha256.Sum256([]byte(record.MessageID))
	case constants.ChatEventTypeExecutionCreated:
		entry.flags |= indexFlagAssistantMessage | indexFlagExecutionFact
		entry.messageHash = sha256.Sum256([]byte(record.ExecutionID))
	case constants.ChatEventTypeExecutionCompleted,
		constants.ChatEventTypeExecutionFailed,
		constants.ChatEventTypeExecutionCanceled:
		entry.flags |= indexFlagExecutionTerminal | indexFlagExecutionFact
	case constants.ChatEventTypeExecutionStarted:
		entry.flags |= indexFlagExecutionFact
	case constants.ChatEventTypeInvocationFinished:
		entry.flags |= indexFlagExecutionFact | indexFlagInvocationFinished
	}
	return entry
}

// isMessageStart 判断索引记录是否建立一条新的用户或伙伴消息。
func (e indexEntry) isMessageStart() bool {
	return e.flags&(indexFlagUserMessage|indexFlagAssistantMessage|indexFlagInheritedMessage) != 0
}

// endOffset 返回对应 JSONL 行结束后的半开字节位置。
func (e indexEntry) endOffset() int64 {
	return e.offset + e.length
}

// indexEntryOffset 返回指定一基序号在索引文件中的字节位置。
func indexEntryOffset(sequence int64) int64 {
	return indexHeaderSize + (sequence-1)*indexEntrySize
}
