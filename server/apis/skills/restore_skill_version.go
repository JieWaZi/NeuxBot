package skills

import (
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalSkills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 恢复版本
type RestoreSkillVersion struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 版本标识
	VersionID string `in:"path" name:"versionId" validate:"required"`
	// 请求体
	Body RestoreSkillVersionRequest `in:"body"`
}

// 恢复版本请求
type RestoreSkillVersionRequest struct {
	// 版本备注
	Notes string `json:"notes"`
}

// Path 返回恢复技能版本接口路径。
func (r *RestoreSkillVersion) Path() string {
	return "/:wakerId/skills/:skillId/versions/:versionId/restore"
}

// Output 复制历史版本目录并追加新的当前版本。
func (r *RestoreSkillVersion) Output(ctx *gin.Context) (interface{}, error) {
	return restoreSkillVersion(ctx, r)
}

// restoreSkillVersion 复用相同资源用例，空伙伴范围表示全局库。
func restoreSkillVersion(ctx *gin.Context, r *RestoreSkillVersion) (models.SkillVersion, error) {
	skill, err := visibleSkill(ctx, r.WakerID, r.SkillID)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	source, err := global.SkillControllerManager.GetVersion(ctx, r.VersionID)
	if err != nil || source.SkillPackageID != skill.ID {
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillVersionNotFound)
	}
	current, err := global.SkillControllerManager.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	nextVersion, ok := nextPatchSemVer(current.Version)
	if !ok {
		return models.SkillVersion{}, ginx.WithStack(status_error.BadRequest)
	}
	relative := filepath.Clean(source.ContentPath)
	if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return models.SkillVersion{}, ginx.WithStack(status_error.BadRequest)
	}
	path := filepath.Join(global.Config.DataDirectory, relative)
	if _, err := os.Stat(path); err != nil {
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillStorageError)
	}
	versionID := util.New()
	contentPath, digest, err := internalSkills.CopyVersion(global.Config.DataDirectory, skill.ID, nextVersion, path)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	metadata, err := internalSkills.ReadMetadata(filepath.Join(global.Config.DataDirectory, contentPath))
	if err != nil {
		_ = internalSkills.RemoveVersion(global.Config.DataDirectory, contentPath)
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	version := models.SkillVersion{ID: versionID, SkillPackageID: skill.ID, Version: nextVersion, Name: metadata.Name, Description: metadata.Description, Operation: "restore", Notes: r.Body.Notes, ContentPath: contentPath, ContentDigest: digest}
	if err := global.SkillControllerManager.CreateVersion(ctx, version, true, current.ID); err != nil {
		_ = internalSkills.RemoveVersion(global.Config.DataDirectory, contentPath)
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, r.SkillID)
	return version, nil
}

func nextPatchSemVer(value string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	if len(parts) != 3 {
		return "", false
	}
	numbers := make([]*big.Int, len(parts))
	for index, part := range parts {
		number, ok := new(big.Int).SetString(part, 10)
		if !ok || number.Sign() < 0 {
			return "", false
		}
		numbers[index] = number
	}
	numbers[2].Add(numbers[2], big.NewInt(1))
	return numbers[0].String() + "." + numbers[1].String() + "." + numbers[2].String(), true
}

func compareSemVer(left string, right string) int {
	parse := func(value string) [3]*big.Int {
		result := [3]*big.Int{new(big.Int), new(big.Int), new(big.Int)}
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
		for index := 0; index < len(parts) && index < 3; index++ {
			if _, ok := result[index].SetString(parts[index], 10); !ok {
				result[index].SetInt64(0)
			}
		}
		return result
	}
	a, b := parse(left), parse(right)
	for index := range a {
		comparison := a[index].Cmp(b[index])
		if comparison > 0 {
			return 1
		}
		if comparison < 0 {
			return -1
		}
	}
	return 0
}
