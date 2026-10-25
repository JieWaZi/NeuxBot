package skills

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalSkills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建版本
type CreateSkillVersion struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body CreateSkillVersionRequest `in:"body"`
}

// 创建版本请求
type CreateSkillVersionRequest struct {
	// 版本号
	Version string `json:"version" validate:"required"`
	// 版本备注
	Notes string `json:"notes"`
	// 文件内容
	Content string `json:"content"`
	// 文件列表
	Files []SkillFileUpdate `json:"files" validate:"max=100"`
	// 资源操作
	ResourceOperations []SkillResourceOperation `json:"resourceOperations" validate:"max=5000"`
}

// 文件更新
type SkillFileUpdate struct {
	// 路径
	Path string `json:"path" validate:"required"`
	// 文件内容
	Content string `json:"content"`
}

// 资源操作
type SkillResourceOperation struct {
	// 操作类型
	Action constants.SkillResourceAction `json:"action" validate:"required"`
	// 路径
	Path string `json:"path" validate:"required"`
	// 目标路径
	TargetPath string `json:"targetPath,omitempty"`
	// 文件内容
	ContentBase64 string `json:"contentBase64,omitempty"`
	// 可执行权限
	Executable bool `json:"executable,omitempty"`
}

// Path 返回手动创建版本接口路径。
func (c *CreateSkillVersion) Path() string { return "/:wakerId/skills/:skillId/versions" }

// Validate 校验版本号和内容大小。
func (c *CreateSkillVersion) Validate(*gin.Context) error {
	if !isSemVer(c.Body.Version) {
		return ginx.WithStack(status_error.SkillContentInvalid)
	}
	if len(c.Body.Content) > 1024*1024 || len(c.Body.Files) > 100 || len(c.Body.ResourceOperations) > 5000 {
		return ginx.WithStack(status_error.SkillContentLimitExceeded)
	}
	for _, file := range c.Body.Files {
		if len(file.Content) > 1024*1024 || !validSkillRelativeFilePath(file.Path) || filepath.ToSlash(filepath.Clean(file.Path)) == "SKILL.md" {
			return ginx.WithStack(status_error.SkillContentInvalid)
		}
	}
	for _, operation := range c.Body.ResourceOperations {
		if !validSkillResourceOperation(operation) {
			return ginx.WithStack(status_error.SkillContentInvalid)
		}
	}
	return nil
}

// Output 复制当前技能目录并写入新的 SKILL.md 版本。
func (c *CreateSkillVersion) Output(ctx *gin.Context) (interface{}, error) {
	return createSkillVersion(ctx, c)
}

// createSkillVersion 复用相同资源用例，空伙伴范围表示全局库。
func createSkillVersion(ctx *gin.Context, c *CreateSkillVersion) (models.SkillVersion, error) {
	skill, err := visibleSkill(ctx, c.WakerID, c.SkillID)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	current, err := global.SkillControllerManager.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	relative := filepath.Clean(current.ContentPath)
	if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillContentInvalid)
	}
	if compareSemVer(c.Body.Version, current.Version) <= 0 {
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillContentInvalid)
	}
	sourcePath := filepath.Join(global.Config.DataDirectory, relative)
	stagingParent := filepath.Join(global.Config.DataDirectory, "skills")
	if err := os.MkdirAll(stagingParent, 0o700); err != nil {
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillStorageError)
	}
	stagingDirectory, err := os.MkdirTemp(stagingParent, "version-staging-")
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillStorageError)
	}
	defer os.RemoveAll(stagingDirectory)
	if err := internalSkills.CopyDirectory(sourcePath, stagingDirectory); err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	target := filepath.Join(stagingDirectory, "SKILL.md")
	if c.Body.Content != "" {
		if err := os.WriteFile(target, []byte(c.Body.Content), 0o600); err != nil {
			return models.SkillVersion{}, ginx.WithStack(status_error.SkillStorageError)
		}
	}
	for _, file := range c.Body.Files {
		if err := writeSkillVersionFile(stagingDirectory, file); err != nil {
			return models.SkillVersion{}, ginx.WithStack(err)
		}
	}
	if err := applySkillResourceOperations(stagingDirectory, c.Body.ResourceOperations); err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	metadata, err := internalSkills.ReadMetadata(stagingDirectory)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	versionID := util.New()
	contentPath, digest, err := internalSkills.CopyVersion(
		global.Config.DataDirectory,
		skill.ID,
		c.Body.Version,
		stagingDirectory,
	)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	version := models.SkillVersion{ID: versionID, SkillPackageID: skill.ID, Version: strings.TrimSpace(c.Body.Version), Name: metadata.Name, Description: metadata.Description, Operation: "edit", Notes: c.Body.Notes, ContentPath: contentPath, ContentDigest: digest}
	if err := global.SkillControllerManager.CreateVersion(ctx, version, true, current.ID); err != nil {
		_ = internalSkills.RemoveVersion(global.Config.DataDirectory, contentPath)
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, c.SkillID)
	return version, nil
}

func validSkillRelativeFilePath(value string) bool {
	relative := filepath.Clean(filepath.FromSlash(strings.TrimSpace(value)))
	return relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func writeSkillVersionFile(root string, update SkillFileUpdate) error {
	if !validSkillRelativeFilePath(update.Path) {
		return status_error.SkillContentInvalid
	}
	relative := filepath.Clean(filepath.FromSlash(strings.TrimSpace(update.Path)))
	target := filepath.Join(root, relative)
	if existing, err := os.Lstat(target); err == nil && (existing.IsDir() || existing.Mode()&os.ModeSymlink != 0) {
		return status_error.SkillContentInvalid
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return status_error.SkillStorageError
	}
	if err := os.WriteFile(target, []byte(update.Content), 0o600); err != nil {
		return status_error.SkillStorageError
	}
	return nil
}

func validSkillResourceOperation(operation SkillResourceOperation) bool {
	if !validSkillRelativeFilePath(operation.Path) || filepath.ToSlash(filepath.Clean(operation.Path)) == "SKILL.md" {
		return false
	}
	if operation.Action == constants.SkillResourceActionMove {
		return validSkillRelativeFilePath(operation.TargetPath) && filepath.ToSlash(filepath.Clean(operation.TargetPath)) != "SKILL.md"
	}
	return strings.TrimSpace(operation.TargetPath) == ""
}

func applySkillResourceOperations(root string, operations []SkillResourceOperation) error {
	for _, operation := range operations {
		if !validSkillResourceOperation(operation) {
			return status_error.SkillContentInvalid
		}
		path := filepath.Join(root, filepath.Clean(filepath.FromSlash(strings.TrimSpace(operation.Path))))
		switch operation.Action {
		case constants.SkillResourceActionMkdir:
			if invalid, err := resourceDestinationInvalid(root, path); err != nil {
				return status_error.SkillStorageError
			} else if invalid {
				return status_error.SkillContentInvalid
			}
			if err := os.MkdirAll(path, 0o700); err != nil {
				return status_error.SkillStorageError
			}
		case constants.SkillResourceActionWrite:
			content, err := base64.StdEncoding.DecodeString(operation.ContentBase64)
			if err != nil {
				return status_error.SkillContentInvalid
			}
			if len(content) > 50*1024*1024 {
				return status_error.SkillContentLimitExceeded
			}
			if invalid, err := resourceDestinationInvalid(root, path); err != nil {
				return status_error.SkillStorageError
			} else if invalid {
				return status_error.SkillContentInvalid
			}
			executable := operation.Executable
			if info, err := os.Lstat(path); err == nil {
				if !info.Mode().IsRegular() {
					return status_error.SkillContentInvalid
				}
				executable = executable || info.Mode().Perm()&0o111 != 0
			} else if !os.IsNotExist(err) {
				return status_error.SkillStorageError
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return status_error.SkillStorageError
			}
			mode := os.FileMode(0o600)
			if executable && pathContainsScriptsDirectory(operation.Path) {
				mode = 0o700
			}
			if err := os.WriteFile(path, content, mode); err != nil {
				return status_error.SkillStorageError
			}
		case constants.SkillResourceActionMove:
			target := filepath.Join(root, filepath.Clean(filepath.FromSlash(strings.TrimSpace(operation.TargetPath))))
			if invalid, err := resourceDestinationInvalid(root, target); err != nil {
				return status_error.SkillStorageError
			} else if invalid {
				return status_error.SkillContentInvalid
			}
			if _, err := os.Lstat(target); err == nil || !os.IsNotExist(err) {
				return status_error.SkillContentInvalid
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return status_error.SkillStorageError
			}
			if err := os.Rename(path, target); err != nil {
				if os.IsNotExist(err) {
					return status_error.SkillContentInvalid
				}
				return status_error.SkillStorageError
			}
		case constants.SkillResourceActionDelete:
			if _, err := os.Lstat(path); err != nil {
				if os.IsNotExist(err) {
					return status_error.SkillContentInvalid
				}
				return status_error.SkillStorageError
			}
			if err := os.RemoveAll(path); err != nil {
				return status_error.SkillStorageError
			}
		default:
			return status_error.SkillContentInvalid
		}
	}
	return nil
}

func resourceDestinationInvalid(root string, target string) (bool, error) {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false, err
	}
	current := root
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	for index := 0; index+1 < len(parts); index++ {
		current = filepath.Join(current, parts[index])
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return false, nil
		}
		if statErr != nil {
			return false, statErr
		}
		if !info.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

func pathContainsScriptsDirectory(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == "scripts" {
			return true
		}
	}
	return false
}
