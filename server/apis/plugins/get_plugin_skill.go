package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 插件技能正文
type PluginSkillResponse struct {
	// 技能资料
	Skill packageplugin.Skill `json:"skill"`
	// 技能正文
	Content string `json:"content"`
	// 资源列表
	Resources []packagefiles.ResourceEntry `json:"resources"`
	// 选中文件
	File *packagefiles.ResourceFile `json:"file,omitempty"`
	// 当前版本
	Version PluginSkillVersion `json:"version"`
	// 历史版本
	Versions []PluginSkillVersion `json:"versions"`
}

// 插件技能版本
type PluginSkillVersion struct {
	// 版本标识
	ID string `json:"id,omitempty"`
	// 插件版本
	Version string `json:"version"`
	// 登记时间
	CreatedAt int64 `json:"createdAt,omitempty"`
	// 技能资料
	Skill packageplugin.Skill `json:"skill"`
}

// 查看插件技能
type GetPluginSkill struct {
	ginx.MethodGet
	// 适用智能体
	Target string `in:"query" name:"target"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 版本标识
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 组件标识
	Key string `in:"query" name:"key" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 资源路径
	FilePath string `in:"query" name:"path"`
}

// Path 返回不可变插件版本的技能查看入口。
func (*GetPluginSkill) Path() string { return "/:pluginId/skill" }

// Output 复验资源可见性、版本归属和内容摘要后读取已发现的技能。
func (o *GetPluginSkill) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	version, err := global.PluginControllerManager.Version(ctx, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if version.PluginID != resource.ID {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	root, err := packageplugin.ContentPath(global.Config.DataDirectory, version.ContentPath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if global.PluginContents != nil {
		release, err := global.PluginContents.Acquire(root)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		defer release()
	}
	if err := packageplugin.VerifyContent(root, version.Digest); err != nil {
		return nil, ginx.WithStack(err)
	}
	skillPackage, skillRoot, err := packageplugin.SkillSource(root, version.Package, o.Target)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	skill, content, err := packageplugin.ReadSkill(skillRoot, skillPackage, o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources, file, err := packageplugin.ReadSkillResources(skillRoot, skillPackage, o.Key, o.FilePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	stored, err := global.PluginControllerManager.Versions(ctx, resource.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	versions := []PluginSkillVersion{}
	for _, retained := range stored {
		for _, component := range retained.Package.Skills {
			if component.Key == o.Key {
				versions = append(versions, PluginSkillVersion{ID: retained.ID, Version: retained.Version, CreatedAt: retained.CreatedAt, Skill: component})
				break
			}
		}
	}
	if o.Target != "" {
		versions = []PluginSkillVersion{{ID: version.ID, Version: version.Version, CreatedAt: version.CreatedAt, Skill: skill}}
	}
	return PluginSkillResponse{
		Skill: skill, Content: content, Resources: resources, File: file,
		Version: PluginSkillVersion{ID: version.ID, Version: version.Version, CreatedAt: version.CreatedAt, Skill: skill}, Versions: versions,
	}, nil
}
