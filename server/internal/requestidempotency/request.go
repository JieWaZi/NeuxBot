// Package requestidempotency 提供资源首次创建请求的稳定摘要、重放判定和资源身份派生。
package requestidempotency

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Identity 是规范化后的首次创建请求身份。
type Identity struct {
	// ID 是客户端为一次逻辑首发稳定复用的标识。
	ID string
	// Hash 是不包含 ID 本身的规范化业务内容摘要。
	Hash string
}

// New 校验客户端标识并为规范化业务内容生成稳定摘要。
func New(clientRequestID string, content any) (Identity, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)
	if clientRequestID == "" || len(clientRequestID) > constants.CreateRequestIDMaximumLength {
		return Identity{}, status_error.BadRequest
	}
	payload, err := json.Marshal(content)
	if err != nil {
		logx.Errorf("marshal create request identity failed: error=%v", err)
		return Identity{}, status_error.InternalServerError
	}
	digest := sha256.Sum256(payload)
	return Identity{
		ID:   clientRequestID,
		Hash: hex.EncodeToString(digest[:]),
	}, nil
}

// ValidateReplay 判断既有资源能否作为本次首次创建请求的确定重放结果。
func (i Identity) ValidateReplay(storedID string, storedHash string, ready bool) error {
	if strings.TrimSpace(storedID) != i.ID || storedHash != i.Hash {
		return status_error.CreateRequestConflict
	}
	if !ready {
		return status_error.CreateRequestInProgress
	}
	return nil
}

// ResourceID 在给定资源命名空间和业务范围内派生 32 位不可逆稳定标识。
func (i Identity) ResourceID(namespace string, scopes ...string) string {
	hasher := sha256.New()
	writePart := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(value))
	}
	writePart(strings.TrimSpace(namespace))
	for _, scope := range scopes {
		writePart(strings.TrimSpace(scope))
	}
	writePart(i.ID)
	return hex.EncodeToString(hasher.Sum(nil)[:16])
}
