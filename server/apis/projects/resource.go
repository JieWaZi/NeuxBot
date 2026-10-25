package projects

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 项目输入
type ProjectInput struct {
	// 名称
	Name string `json:"name" validate:"required,max=80"`
	// 简介
	Brief string `json:"brief" validate:"max=500"`
	// 目录列表
	Directories []ProjectDirectoryInput `json:"directories" validate:"required,min=1,dive"`
}

// 项目目录输入
type ProjectDirectoryInput struct {
	// 路径
	Path string `json:"path" validate:"required,max=4096"`
	// 加载指令
	LoadInstructions *bool `json:"loadInstructions"`
	// 默认目录
	IsDefault bool `json:"isDefault"`
}

// 项目目录
type ProjectSourceResource struct {
	// 标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
	// 路径
	Path string `json:"path"`
	// 可用状态
	Available bool `json:"available"`
	// 加载指令
	LoadInstructions bool `json:"loadInstructions"`
	// 默认目录
	IsDefault bool `json:"isDefault"`
}

// 项目资源
type ProjectResource struct {
	// 标识
	ID string `json:"id"`
	// 伙伴标识
	WakerID string `json:"wakerId"`
	// 名称
	Name string `json:"name"`
	// 简介
	Brief string `json:"brief"`
	// 目录列表
	Sources []ProjectSourceResource `json:"sources"`
	// 可用状态
	Available bool `json:"available"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

func normalizeProjectInput(input ProjectInput) (ProjectInput, error) {
	input.Name = util.NormalizeName(input.Name)
	input.Brief = strings.TrimSpace(input.Brief)
	if input.Name == "" || len(input.Directories) == 0 {
		return ProjectInput{}, status_error.BadRequest
	}
	defaultCount := 0
	for index := range input.Directories {
		input.Directories[index].Path = strings.TrimSpace(input.Directories[index].Path)
		if input.Directories[index].Path == "" {
			return ProjectInput{}, status_error.BadRequest
		}
		if input.Directories[index].LoadInstructions == nil {
			enabled := true
			input.Directories[index].LoadInstructions = &enabled
		}
		if input.Directories[index].IsDefault {
			defaultCount++
		}
	}
	if defaultCount == 0 {
		input.Directories[0].IsDefault = true
	}
	if defaultCount > 1 {
		return ProjectInput{}, status_error.BadRequest
	}
	return input, nil
}

func resolveProjectDirectories(input ProjectInput) ([]models.ProjectDirectory, error) {
	directories := make([]models.ProjectDirectory, 0, len(input.Directories))
	seen := make(map[string]struct{}, len(input.Directories))
	for _, directory := range input.Directories {
		source, resolveErr := projectsource.Resolve(directory.Path)
		if resolveErr != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		if _, exists := seen[source.ID]; exists {
			return nil, status_error.BadRequest
		}
		seen[source.ID] = struct{}{}
		loadInstructions := true
		if directory.LoadInstructions != nil {
			loadInstructions = *directory.LoadInstructions
		}
		directories = append(directories, models.ProjectDirectory{
			Source:           source,
			LoadInstructions: loadInstructions,
			IsDefault:        directory.IsDefault,
		})
	}
	return directories, nil
}

func projectResource(
	project models.Project,
	directories []models.ProjectDirectory,
) ProjectResource {
	resources := make([]ProjectSourceResource, 0, len(directories))
	available := len(directories) > 0
	for _, directory := range directories {
		source := directory.Source
		sourceAvailable := projectsource.Available(source)
		available = available && sourceAvailable
		resources = append(resources, ProjectSourceResource{
			ID:               source.ID,
			Name:             source.Name,
			Path:             source.Path,
			Available:        sourceAvailable,
			LoadInstructions: directory.LoadInstructions,
			IsDefault:        directory.IsDefault,
		})
	}
	return ProjectResource{
		ID:        project.ID,
		WakerID:   project.WakerID,
		Name:      project.Name,
		Brief:     project.Brief,
		Sources:   resources,
		Available: available,
		CreatedAt: project.CreatedAt,
		UpdatedAt: project.UpdatedAt,
	}
}
