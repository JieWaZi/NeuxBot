package eventlog_next

import (
	"database/sql/driver"
	"encoding/hex"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	indexHexIdentity byte = iota
	indexPrefixedIdentity
	indexOpaqueIdentity
)

// indexIdentity keeps existing long hexadecimal IDs compact in derived keys.
type indexIdentity string

func (indexIdentity) GormDataType() string { return "blob" }

func (id indexIdentity) Value() (driver.Value, error) {
	text := string(id)
	if len(text) == 32 {
		var packed [16]byte
		if _, err := hex.Decode(packed[:], []byte(text)); err == nil && hex.EncodeToString(packed[:]) == text {
			return append([]byte{indexHexIdentity}, packed[:]...), nil
		}
		// Older producers also use a one-character namespace on a 31-digit hex ID.
		suffix := "0" + text[1:]
		if _, err := hex.Decode(packed[:], []byte(suffix)); err == nil && hex.EncodeToString(packed[:]) == suffix {
			return append([]byte{indexPrefixedIdentity, text[0]}, packed[:]...), nil
		}
	}
	return append([]byte{indexOpaqueIdentity}, text...), nil
}

func (id *indexIdentity) Scan(value any) error {
	raw, ok := value.([]byte)
	if !ok || len(raw) == 0 {
		return status_error.ChatLogIndexCorrupted
	}
	switch raw[0] {
	case indexHexIdentity:
		if len(raw) == 17 {
			*id = indexIdentity(hex.EncodeToString(raw[1:]))
			return nil
		}
	case indexPrefixedIdentity:
		if len(raw) == 18 && raw[2] < 16 {
			encoded := hex.EncodeToString(raw[2:])
			*id = indexIdentity(string(raw[1:2]) + encoded[1:])
			return nil
		}
	case indexOpaqueIdentity:
		*id = indexIdentity(raw[1:])
		return nil
	}
	return status_error.ChatLogIndexCorrupted
}
