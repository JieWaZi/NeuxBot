package util

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// NewAutomaticTaskToken 生成只在签发响应返回的密钥及用于持久化的单向摘要。
func NewAutomaticTaskToken() (string, []byte, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		logx.Errorf("generate automatic task token failed: error=%v", err)
		return "", nil, status_error.AutomaticTaskTokenGenerationFailed
	}
	token := base64.RawURLEncoding.EncodeToString(data)
	digest := sha256.Sum256([]byte(token))
	return token, digest[:], nil
}

// AutomaticTaskTokenMatches 以固定长度比较验证密钥，空值和缺失摘要均拒绝。
func AutomaticTaskTokenMatches(token string, tokenHash []byte) bool {
	digest := sha256.Sum256([]byte(token))
	expected := make([]byte, sha256.Size)
	if len(tokenHash) == sha256.Size {
		copy(expected, tokenHash)
	}
	valid := subtle.ConstantTimeCompare(digest[:], expected)
	return valid == 1 && token != "" && len(tokenHash) == sha256.Size
}
