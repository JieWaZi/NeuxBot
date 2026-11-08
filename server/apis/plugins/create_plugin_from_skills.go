package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建技能插件
type CreatePluginFromSkills struct {
	ginx.MethodPost
	// 请求体
	Body CreatePluginFromSkillsRequest `in:"body"`
}

// 创建技能插件请求
type CreatePluginFromSkillsRequest struct {
	// 批次令牌
	Token string `json:"token" validate:"required"`
	// 候选标识
	CandidateIDs []string `json:"candidateIds" validate:"required,min=2,max=100,dive,required"`
	// 插件名称
	Name string `json:"name" validate:"required,max=120"`
	// 插件说明
	Description string `json:"description"`
}

// Path 返回从已扫描技能创建本地插件的入口。
func (*CreatePluginFromSkills) Path() string { return "/from-skills" }

// Validate 只校验名称和选择集合，不查询或重新扫描来源。
func (o *CreatePluginFromSkills) Validate(*gin.Context) error {
	o.Body.Name = util.NormalizeName(o.Body.Name)
	o.Body.Description = strings.TrimSpace(o.Body.Description)
	if o.Body.Token == "" || o.Body.Name == "" || utf8.RuneCountInString(o.Body.Name) > 120 || len(o.Body.CandidateIDs) < 2 || len(o.Body.CandidateIDs) > 100 {
		return status_error.BadRequest
	}
	seen := map[string]bool{}
	for _, id := range o.Body.CandidateIDs {
		if id == "" || seen[id] {
			return status_error.BadRequest
		}
		seen[id] = true
	}
	return nil
}

// Output 将所选完整技能保存为一个标准插件；伙伴安装继续通过既有显式操作完成。
func (o *CreatePluginFromSkills) Output(ctx *gin.Context) (interface{}, error) {
	if err := o.Validate(ctx); err != nil {
		return nil, ginx.WithStack(err)
	}
	batch, err := skills.OpenStagedImport(filepath.Join(global.Config.DataDirectory, "skills", "tmp"), o.Body.Token)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	id := util.New()
	stage, provenance, err := packageplugin.BuildSkillBundle(stagedRoot(), batch, o.Body.CandidateIDs, id, o.Body.Name, o.Body.Description)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer os.RemoveAll(filepath.Join(stagedRoot(), stage.Token))
	resource, version, err := persistPreparedResource(ctx, stage, false, models.Plugin{ID: id, Name: o.Body.Name, Description: o.Body.Description, SourceKey: constants.PluginLocalSkillSourcePrefix + id}, provenance)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := os.RemoveAll(batch.Root); err != nil {
		logx.Errorf("remove bundled skill batch failed: error=%v", err)
	}
	return pluginDetail(ctx, resource, version)
}
