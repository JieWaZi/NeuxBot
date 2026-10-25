package skills

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取版本
type GetSkillVersion struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 版本标识
	VersionID string `in:"path" name:"versionId" validate:"required"`
}

// 版本响应
type SkillVersionResponse struct {
	// 版本信息
	Version models.SkillVersion `json:"version"`
	// 文件内容
	Content string `json:"content"`
	// 资源列表
	Resources []SkillResourceEntry `json:"resources"`
}

// 资源条目
type SkillResourceEntry struct {
	// 路径
	Path string `json:"path"`
	// 是否目录
	Directory bool `json:"directory"`
	// 字节数
	Size int64 `json:"size,omitempty"`
	// 内容摘要
	Digest string `json:"digest,omitempty"`
	// 可执行权限
	Executable bool `json:"executable,omitempty"`
}

// 获取版本文件
type GetSkillVersionFile struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 版本标识
	VersionID string `in:"path" name:"versionId" validate:"required"`
	// 文件路径
	FilePath string `in:"query" name:"path" validate:"required"`
}

// 版本文件响应
type SkillVersionFileResponse struct {
	// 路径
	Path string `json:"path"`
	// 文件内容
	Content string `json:"content,omitempty"`
	// 二进制文件
	Binary bool `json:"binary"`
	// 字节数
	Size int64 `json:"size"`
	// Base64 内容
	ContentBase64 string `json:"contentBase64,omitempty"`
	// 可执行权限
	Executable bool `json:"executable,omitempty"`
}

// Path 返回技能版本详情接口路径。
func (g *GetSkillVersion) Path() string { return "/:wakerId/skills/:skillId/versions/:versionId" }

// Output 返回版本元数据和内容。
func (g *GetSkillVersion) Output(ctx *gin.Context) (interface{}, error) {
	return getSkillVersion(ctx, g)
}

// getSkillVersion 复用相同资源用例，空伙伴范围表示全局库。
func getSkillVersion(ctx *gin.Context, g *GetSkillVersion) (SkillVersionResponse, error) {
	skill, err := visibleSkill(ctx, g.WakerID, g.SkillID)
	if err != nil {
		return SkillVersionResponse{}, ginx.WithStack(err)
	}
	version, err := global.SkillControllerManager.GetVersion(ctx, g.VersionID)
	if err != nil || version.SkillPackageID != skill.ID {
		return SkillVersionResponse{}, ginx.WithStack(status_error.SkillVersionNotFound)
	}
	relative := filepath.Clean(version.ContentPath)
	if filepath.IsAbs(relative) || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return SkillVersionResponse{}, ginx.WithStack(status_error.BadRequest)
	}
	versionDirectory := filepath.Join(global.Config.DataDirectory, relative)
	content, err := os.ReadFile(filepath.Join(versionDirectory, "SKILL.md"))
	if err != nil {
		return SkillVersionResponse{}, ginx.WithStack(status_error.SkillStorageError)
	}
	resources, err := listSkillResources(versionDirectory)
	if err != nil {
		return SkillVersionResponse{}, ginx.WithStack(status_error.SkillStorageError)
	}
	return SkillVersionResponse{
		Version:   version,
		Content:   string(content),
		Resources: resources,
	}, nil
}

// Path 返回技能版本文件接口路径。
func (g *GetSkillVersionFile) Path() string {
	return "/:wakerId/skills/:skillId/versions/:versionId/file"
}

// Output 返回经过可见性和版本归属校验的文件内容。
func (g *GetSkillVersionFile) Output(ctx *gin.Context) (interface{}, error) {
	return getSkillVersionFile(ctx, g)
}

// getSkillVersionFile 复用相同资源用例，空伙伴范围表示全局库。
func getSkillVersionFile(ctx *gin.Context, g *GetSkillVersionFile) (SkillVersionFileResponse, error) {
	skill, err := visibleSkill(ctx, g.WakerID, g.SkillID)
	if err != nil {
		return SkillVersionFileResponse{}, ginx.WithStack(err)
	}
	version, err := global.SkillControllerManager.GetVersion(ctx, g.VersionID)
	if err != nil || version.SkillPackageID != skill.ID {
		return SkillVersionFileResponse{}, ginx.WithStack(status_error.SkillVersionNotFound)
	}
	root, err := skillVersionDirectory(version.ContentPath)
	if err != nil {
		return SkillVersionFileResponse{}, ginx.WithStack(err)
	}
	file, err := readSkillVersionFile(root, g.FilePath)
	if err != nil {
		return SkillVersionFileResponse{}, ginx.WithStack(err)
	}
	return file, nil
}

func listSkillResources(root string) ([]SkillResourceEntry, error) {
	resources := make([]SkillResourceEntry, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return status_error.BadRequest
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		resource := SkillResourceEntry{
			Path:      filepath.ToSlash(relative),
			Directory: entry.IsDir(),
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(content)
			resource.Size = int64(len(content))
			resource.Digest = hex.EncodeToString(digest[:])
			resource.Executable = info.Mode().Perm()&0o111 != 0
		}
		resources = append(resources, resource)
		return nil
	})
	return resources, err
}

func skillVersionDirectory(contentPath string) (string, error) {
	relative := filepath.Clean(contentPath)
	if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", status_error.BadRequest
	}
	return filepath.Join(global.Config.DataDirectory, relative), nil
}

func readSkillVersionFile(root string, requestedPath string) (SkillVersionFileResponse, error) {
	relative := filepath.Clean(filepath.FromSlash(strings.TrimSpace(requestedPath)))
	if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return SkillVersionFileResponse{}, status_error.BadRequest
	}
	path := filepath.Join(root, relative)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return SkillVersionFileResponse{}, status_error.BadRequest
	}
	if err != nil {
		return SkillVersionFileResponse{}, status_error.SkillStorageError
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return SkillVersionFileResponse{}, status_error.BadRequest
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return SkillVersionFileResponse{}, status_error.SkillStorageError
	}
	binary := !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0
	response := SkillVersionFileResponse{
		Path:       filepath.ToSlash(relative),
		Binary:     binary,
		Size:       int64(len(content)),
		Executable: info.Mode().Perm()&0o111 != 0,
	}
	if !binary {
		response.Content = string(content)
	} else {
		response.ContentBase64 = base64.StdEncoding.EncodeToString(content)
	}
	return response, nil
}
