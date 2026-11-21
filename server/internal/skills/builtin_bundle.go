package skills

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"runtime"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

//go:embed builtin/bundle.generated.json
var builtinBundle []byte

// BuiltinDefinition 保存仓库发布清单和完整技能资源，不依赖运行时的源码目录。
type BuiltinDefinition struct {
	// System 表示每次受管执行必备、不进入普通技能编辑关系的系统技能。
	System bool `json:"system,omitempty"`
	// ID 是跨应用重启保持稳定的技能身份。
	ID string `json:"id"`
	// Directory 是技能声明名及仓库内目录名称。
	Directory string `json:"directory"`
	// Version 是首次安装和内容刷新时采用的最低发布版本。
	Version string `json:"version"`
	// Files 保存该技能的全部资源和执行位。
	Files []BuiltinResource `json:"files"`
}

// BuiltinResource 是生成器打包的一个普通文件。
type BuiltinResource struct {
	// Path 是相对于技能目录的安全路径。
	Path string `json:"path"`
	// Data 是 JSON 中以 Base64 保存的原始字节。
	Data []byte `json:"data"`
	// Executable 表示资源需要保留脚本执行位。
	Executable bool `json:"executable"`
}

// BuiltinDefinitions 解码随 Core 发布的技能及资源。
func BuiltinDefinitions() ([]BuiltinDefinition, error) {
	var content struct {
		// Skills 是构建时从仓库根 skills/ 生成的目录。
		Skills []BuiltinDefinition `json:"skills"`
	}
	if err := json.Unmarshal(builtinBundle, &content); err != nil {
		logx.Errorf("decode builtin skill bundle failed: error=%v", err)
		return nil, status_error.SkillContentInvalid
	}
	return content.Skills, nil
}

// SystemDocumentFingerprint 描述随应用交付的一个系统技能文档。
type SystemDocumentFingerprint struct {
	// Digest 是文档内容的 SHA-256 摘要。
	Digest [sha256.Size]byte
	// Bytes 是文档的原始字节数。
	Bytes int64
	// Executable 表示平台文件元数据中是否预期有执行位。
	Executable bool
}

var systemDocumentFingerprints = sync.OnceValue(func() map[string]SystemDocumentFingerprint {
	item, err := SystemSkillDefinition()
	if err != nil {
		return nil
	}
	documents := make(map[string]SystemDocumentFingerprint, len(item.Files))
	for _, file := range item.Files {
		if IsSystemSkillDocument(file.Path) {
			documents[file.Path] = SystemDocumentFingerprint{
				Digest: sha256.Sum256(file.Data), Bytes: int64(len(file.Data)), Executable: file.Executable && runtime.GOOS != "windows",
			}
		}
	}
	return documents
})

// SystemDocument 查找原生投影的原始文档指纹，不依赖工作区或发布目录的副本。
func SystemDocument(relative string) (SystemDocumentFingerprint, bool) {
	document, exists := systemDocumentFingerprints()[relative]
	return document, exists
}

// SystemSkillDefinition 返回随 Core 交付的系统技能。
func SystemSkillDefinition() (BuiltinDefinition, error) {
	items, err := BuiltinDefinitions()
	if err != nil {
		return BuiltinDefinition{}, err
	}
	for _, item := range items {
		if item.System && item.Directory == constants.AgentCLISkillName {
			return item, nil
		}
	}
	return BuiltinDefinition{}, status_error.SkillUnavailable
}
