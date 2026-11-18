package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	// MaterializationManifestName 是受管工作区中记录物化技能身份的文件名。
	MaterializationManifestName = ".neuxbot-skills.json"
	// MaterializationManifestVersion 是当前运行时物化记录的存储版本。
	MaterializationManifestVersion = 1
	// MaterializationManifestMaximumBytes 限制物化记录允许的 JSON 字节数。
	MaterializationManifestMaximumBytes = 1 << 20
)

// MaterializationManifest 描述每个原生发现目录中由运行时物化的技能。
type MaterializationManifest struct {
	// Version 标识能安全解析和使用的物化记录格式。
	Version int `json:"version"`
	// Targets 按原生发现目录保存已物化的技能名与完整内容摘要。
	Targets map[string]map[string]string `json:"targets"`
}

// ParseMaterializationManifest 校验现有记录的版本、目标路径和技能内容摘要。
func ParseMaterializationManifest(data []byte) (MaterializationManifest, error) {
	var result MaterializationManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > MaterializationManifestMaximumBytes ||
		decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF ||
		result.Version != MaterializationManifestVersion || result.Targets == nil {
		return MaterializationManifest{}, status_error.SkillContentInvalid
	}
	for directory, entries := range result.Targets {
		native, base, split := strings.Cut(directory, "/")
		if !split || !safeMaterializationSegment(native) || base != "skills" || entries == nil {
			return MaterializationManifest{}, status_error.SkillContentInvalid
		}
		for name, digest := range entries {
			decoded, err := hex.DecodeString(digest)
			if !safeMaterializationSegment(name) || err != nil || len(decoded) != sha256.Size {
				return MaterializationManifest{}, status_error.SkillContentInvalid
			}
		}
	}
	return result, nil
}

func safeMaterializationSegment(value string) bool {
	return value != "" && value != "." && value != ".." &&
		filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
}
