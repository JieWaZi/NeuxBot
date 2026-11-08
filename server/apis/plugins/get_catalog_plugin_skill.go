package plugins

import (
	"crypto/sha256"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查看目录插件技能
type GetCatalogPluginSkill struct {
	ginx.MethodGet
	// 来源提交
	SourceCommit string `in:"query" name:"sourceCommit"`
	// 压缩摘要
	ArchiveSHA256 string `in:"query" name:"archiveSHA256"`
	// 目录条目
	Key string `in:"path" name:"key" validate:"required"`
	// 组件标识
	SkillKey string `in:"query" name:"skillKey" validate:"required"`
	// 资源路径
	FilePath string `in:"query" name:"path"`
}

// Path 返回未安装目录插件的技能查看入口。
func (*GetCatalogPluginSkill) Path() string { return "/catalog/:key/skill" }

// Output 仅读取固定发布资料，不下载或执行归档。
func (o *GetCatalogPluginSkill) Output(ctx *gin.Context) (interface{}, error) {
	entry, err := catalog.Find(global.PluginCatalogSource(), o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if (o.SourceCommit != "" && o.SourceCommit != entry.SourceCommit) || (o.ArchiveSHA256 != "" && o.ArchiveSHA256 != entry.ArchiveSHA256) {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if o.FilePath != "" && o.FilePath != "SKILL.md" {
		return nil, ginx.WithStack(status_error.PluginContentInvalid)
	}
	details, err := catalog.ReadDetails(entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	skill, content, err := details.Skill(o.SkillKey)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	version := PluginSkillVersion{Version: details.Package.Manifest.Version, Skill: skill}
	resources := []packagefiles.ResourceEntry{{Path: "SKILL.md", Size: int64(len(content)), Digest: fmt.Sprintf("%x", sha256.Sum256([]byte(content)))}}
	result := PluginSkillResponse{Skill: skill, Content: content, Resources: resources, Version: version, Versions: []PluginSkillVersion{version}}
	if o.FilePath == "SKILL.md" {
		result.File = &packagefiles.ResourceFile{Path: "SKILL.md", Content: content, Size: int64(len(content))}
	}
	return result, nil
}
