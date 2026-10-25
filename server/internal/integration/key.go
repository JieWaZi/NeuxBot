// Package integration 提供集成凭据、授权集合和监听的内部能力。
package integration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Copyright (c) Tencent. MIT License，完整许可见根目录 THIRD_PARTY_NOTICES.md。

// GenerateKey 生成高熵秘密，不包含主体 ID。
func GenerateKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		logx.Errorf("generate integration credential: %v", err)
		return "", status_error.IntegrationCredentialError
	}
	return "sk-" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashKey 返回完整凭据摘要，供唯一索引查询使用。
func HashKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// normalizeIDs 过滤空值并保序去重，空集不表示全资源。
func normalizeIDs(in []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
