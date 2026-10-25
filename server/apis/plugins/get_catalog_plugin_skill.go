package plugins

import (
	"crypto/sha256"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查看目录插件技能
type GetCatalogPluginSkill struct {
	ginx.MethodGet
	// 适用智能体
	Target string `in:"query" name:"target"`
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

// Output 复验分发归档并读取临时快照，不登记资源或安装关系。
func (o *GetCatalogPluginSkill) Output(ctx *gin.Context) (interface{}, error) {
	entry, err := catalog.Find(global.PluginCatalogSource(), o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if (o.SourceCommit != "" && o.SourceCommit != entry.SourceCommit) || (o.ArchiveSHA256 != "" && o.ArchiveSHA256 != entry.ArchiveSHA256) {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if entry.DetailsURL != "" {
		if (o.Target != "" && o.Target != "codex" && o.Target != "claude") || (o.FilePath != "" && o.FilePath != "SKILL.md") {
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
	archive, err := catalog.ResolveEntry(global.PluginCatalogSource(), entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	stage, err := packageplugin.StageSource(stagedRoot(), archive, entry.PackagePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer os.RemoveAll(filepath.Join(stagedRoot(), stage.Token))
	skillPackage, skillRoot, err := packageplugin.SkillSource(stage.Root, stage.Package, o.Target)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	skill, content, err := packageplugin.ReadSkill(skillRoot, skillPackage, o.SkillKey)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources, file, err := packageplugin.ReadSkillResources(skillRoot, skillPackage, o.SkillKey, o.FilePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	version := PluginSkillVersion{Version: stage.Package.Manifest.Version, Skill: skill}
	return PluginSkillResponse{Skill: skill, Content: content, Resources: resources, File: file, Version: version, Versions: []PluginSkillVersion{version}}, nil
}
