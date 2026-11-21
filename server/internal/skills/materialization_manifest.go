package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	// MaterializationWorkspaceDirectory 保存工作区内由宿主管理的资源。
	MaterializationWorkspaceDirectory = ".neuxbot"
	// MaterializationDiscoveryDirectory 是原生技能目录和宿主快照目录的末段。
	MaterializationDiscoveryDirectory = "skills"
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
	// Targets 按原生发现目录保存已物化的技能名与投影内容摘要。
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
		if !split || !safeMaterializationSegment(native) || base != MaterializationDiscoveryDirectory || entries == nil {
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

// MaterializationSnapshotPath 返回以工作区为根的投影目录，使用斜线分隔路径。
func MaterializationSnapshotPath(digest string) string {
	return MaterializationWorkspaceDirectory + "/" + MaterializationDiscoveryDirectory + "/" + digest
}

// IsSystemSkillDocument 筛选原生系统技能投影包含的路径，不投影 CLI 程序。
func IsSystemSkillDocument(relative string) bool {
	name := filepath.Base(relative)
	if strings.EqualFold(name, constants.AgentCLIBinaryName) || strings.EqualFold(name, constants.AgentCLIBinaryName+".exe") {
		return false
	}
	return relative == "SKILL.md" || relative == "references" || strings.HasPrefix(relative, "references/")
}

func safeMaterializationSegment(value string) bool {
	return value != "" && value != "." && value != ".." &&
		filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
}
