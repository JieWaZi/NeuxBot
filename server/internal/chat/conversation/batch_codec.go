// Package conversation owns the Chat and native JSONL streams.
package conversation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"strconv"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

const (
	wireVersion      = 2
	maxBatchRows     = 128
	maxBatchBytes    = 16 << 20
	maxIdentityBytes = 512
)

// location is the exact extent of one row inside a committed frame.
type location struct {
	// Offset is the first byte of the JSON value.
	Offset int64
	// Length excludes the enclosing frame and delimiter.
	Length int
	// Checksum detects a modified row after the index was derived.
	Checksum uint32
}

// batchHeader holds the request identity and the original receipt.
type batchHeader struct {
	// Key identifies the logical command.
	Key string
	// Request is the digest of the semantic input before disk encoding.
	Request [32]byte
	// Receipt identifies the durable range.
	Receipt agent.CommitReceipt
	// Where locates the small authoritative receipt header independently of its rows.
	Where location
}

func validID(s string) bool {
	return len(s) > 0 && len(s) <= maxIdentityBytes && utf8.ValidString(s)
}

func rowLocation(offset int64, raw []byte) location {
	return location{Offset: offset, Length: len(raw), Checksum: crc32.ChecksumIEEE(raw)}
}

// encodeBatch puts the checksum after all the rows. A missing newline is never a commit.
func encodeBatch(header batchHeader, rows [][]byte, offset int64) ([]byte, []location, error) {
	if !validID(header.Key) || len(rows) == 0 || len(rows) > maxBatchRows {
		return nil, nil, status_error.ChatLogRecordInvalid
	}
	key, err := json.Marshal(header.Key)
	if err != nil {
		return nil, nil, status_error.ChatLogRecordInvalid
	}
	frame := strconv.AppendInt([]byte{'['}, wireVersion, 10)
	frame = append(frame, ',')
	frame = append(frame, key...)
	frame = append(frame, ',')
	frame = strconv.AppendInt(frame, header.Receipt.First-1, 10)
	frame = append(frame, ',')
	frame = strconv.AppendInt(frame, header.Receipt.RecordedAt, 10)
	frame = append(frame, ',', '"')
	frame = hex.AppendEncode(frame, header.Request[:])
	frame = append(frame, '"', ',')
	frame = strconv.AppendInt(frame, int64(len(rows)), 10)
	frame = append(frame, ',', '[')
	positions := make([]location, 0, len(rows))
	for i, row := range rows {
		if i > 0 {
			frame = append(frame, ',')
		}
		if len(row) > maxBatchBytes-len(frame) {
			return nil, nil, status_error.ChatLogRecordInvalid
		}
		positions = append(positions, rowLocation(offset+int64(len(frame)), row))
		frame = append(frame, row...)
	}
	frame = append(frame, ']')
	sum := sha256.Sum256(frame)
	frame = append(frame, ',', '"')
	frame = hex.AppendEncode(frame, sum[:])
	frame = append(frame, '"', ']', '\n')
	if len(frame) > maxBatchBytes {
		return nil, nil, status_error.ChatLogRecordInvalid
	}
	return frame, positions, nil
}

// decodeBatch validates a complete line and returns independent row coordinates.
func decodeBatch(line []byte, offset, before int64, visit func([]byte, location) error) (batchHeader, error) {
	var result batchHeader
	const trailerBytes = 69
	if len(line) <= trailerBytes || len(line) > maxBatchBytes || line[len(line)-1] != '\n' {
		return result, status_error.ChatLogCorrupted
	}
	bodyEnd := len(line) - trailerBytes
	if !bytes.Equal(line[bodyEnd:bodyEnd+2], []byte{',', '"'}) ||
		!bytes.Equal(line[len(line)-3:], []byte{'"', ']', '\n'}) {
		return result, status_error.ChatLogCorrupted
	}
	sum := sha256.Sum256(line[:bodyEnd])
	var expected [64]byte
	hex.Encode(expected[:], sum[:])
	if !bytes.Equal(expected[:], line[bodyEnd+2:len(line)-3]) {
		return result, status_error.ChatLogCorrupted
	}
	d := json.NewDecoder(bytes.NewReader(line[:bodyEnd]))
	result, err := readBatchPrefix(d)
	if err != nil || result.Receipt.First != before+1 {
		return batchHeader{}, status_error.ChatLogCorrupted
	}
	count := 0
	for d.More() {
		var span jsonSpan
		if d.Decode(&span) != nil || count == maxBatchRows {
			return result, status_error.ChatLogCorrupted
		}
		end := d.InputOffset()
		start := end - int64(span)
		if count == 0 {
			result.Where = rowLocation(offset, line[:start])
		}
		raw := line[start:end]
		if err := visit(raw, rowLocation(offset+start, raw)); err != nil {
			return result, err
		}
		count++
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') || int64(count) != result.Receipt.Version-result.Receipt.First+1 || d.InputOffset() != int64(bodyEnd) {
		return result, status_error.ChatLogCorrupted
	}
	return result, nil
}

// jsonSpan records the decoded byte extent without making a second copy of a row.
type jsonSpan int

func (s *jsonSpan) UnmarshalJSON(raw []byte) error {
	*s = jsonSpan(len(raw))
	return nil
}

func readBatchPrefix(d *json.Decoder) (batchHeader, error) {
	var result batchHeader
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return result, status_error.ChatLogCorrupted
	}
	var version, count int
	var base int64
	var request string
	if d.Decode(&version) != nil || version != wireVersion ||
		d.Decode(&result.Key) != nil || !validID(result.Key) ||
		d.Decode(&base) != nil || base < 0 ||
		d.Decode(&result.Receipt.RecordedAt) != nil || result.Receipt.RecordedAt <= 0 ||
		d.Decode(&request) != nil || len(request) != 64 ||
		d.Decode(&count) != nil || count <= 0 || count > maxBatchRows {
		return result, status_error.ChatLogCorrupted
	}
	if _, err := hex.Decode(result.Request[:], []byte(request)); err != nil {
		return result, status_error.ChatLogCorrupted
	}
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return result, status_error.ChatLogCorrupted
	}
	result.Receipt.First = base + 1
	result.Receipt.Version = base + int64(count)
	if result.Receipt.Version < result.Receipt.First {
		return batchHeader{}, status_error.ChatLogCorrupted
	}
	return result, nil
}

func decodeBatchPrefix(data []byte) (batchHeader, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	result, err := readBatchPrefix(d)
	if err != nil || d.InputOffset() != int64(len(data)) {
		return batchHeader{}, status_error.ChatLogCorrupted
	}
	return result, nil
}
